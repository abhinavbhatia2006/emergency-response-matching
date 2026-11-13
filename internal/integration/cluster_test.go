package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/testdb"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/coordinator"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/node"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

func TestSixNodeFailureRecoveryAndDistribution(t *testing.T) {
	const token = "cluster-test-secret"
	ctx := context.Background()
	var configs []coordinator.NodeConfig
	var servers []*httptest.Server
	var paths []string
	var stores []*storage.Store
	for shard := 0; shard < 3; shard++ {
		for copy := 0; copy < 2; copy++ {
			path := testdb.DSN(t)
			store, err := storage.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(store.Close)
			service, err := node.New(store, shard, 3, token)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(service.Handler())
			t.Cleanup(server.Close)
			configs = append(configs, coordinator.NodeConfig{ID: fmt.Sprintf("s%d-%d", shard, copy), URL: server.URL, Shard: shard})
			servers = append(servers, server)
			paths = append(paths, path)
			stores = append(stores, store)
		}
	}
	journal, err := storage.Open(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(journal.Close)
	c, err := coordinator.New(journal, configs, token, time.Minute, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range configs {
		if err := c.Register(ctx, n.ID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("request/%d", i)
		if _, err := c.Put(ctx, key, json.RawMessage(`{"status":"pending"}`), 0); err != nil {
			t.Fatal(err)
		}
	}
	for i, store := range stores {
		records, err := store.Records()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) == 0 {
			t.Fatalf("node %d has no data", i)
		}
		for _, r := range records {
			shard, _ := storage.Shard(r.Key, 3)
			if shard != configs[i].Shard {
				t.Fatalf("misrouted %s", r.Key)
			}
		}
	}
	key := "request/0"
	shard, _ := storage.Shard(key, 3)
	failedIndex := shard * 2
	address := servers[failedIndex].Listener.Addr().String()
	servers[failedIndex].Close()
	if _, err := c.Get(ctx, key); err != nil {
		t.Fatalf("read during failure: %v", err)
	}
	if _, err := c.Put(ctx, key, json.RawMessage(`{"status":"matched"}`), 1); err != nil {
		t.Fatalf("write during failure: %v", err)
	}
	deleteKey := ""
	staleRecords, err := stores[failedIndex].Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range staleRecords {
		if r.Key != key {
			deleteKey = r.Key
			break
		}
	}
	if deleteKey == "" {
		t.Fatal("fixture needs second key")
	}
	if _, err := c.Delete(ctx, deleteKey, 1); err != nil {
		t.Fatal(err)
	}
	// Reopen the stopped node's PostgreSQL state and reconnect on its configured address.
	recoveredStore, err := storage.Open(paths[failedIndex])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recoveredStore.Close)
	old, _ := recoveredStore.Get(key)
	if old.Version != 1 {
		t.Fatal("fixture was not stale")
	}
	recoveredNode, err := node.New(recoveredStore, shard, 3, token)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewUnstartedServer(recoveredNode.Handler())
	restarted.Listener = listener
	restarted.Start()
	defer restarted.Close()
	if err := c.Register(ctx, configs[failedIndex].ID); err != nil {
		t.Fatal(err)
	}
	got, err := recoveredStore.Get(key)
	if err != nil || got.Version != 2 {
		t.Fatalf("failed recovery: %+v %v", got, err)
	}
	if _, err := recoveredStore.Get(deleteKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted record resurrected: %v", err)
	}
	tombstone := false
	recoveredRecords, err := recoveredStore.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recoveredRecords {
		if r.Key == deleteKey && r.Deleted && r.Version == 2 {
			tombstone = true
		}
	}
	if !tombstone {
		t.Fatal("missing repaired tombstone")
	}
	servers[failedIndex+1].Close()
	if got, err := c.Get(ctx, key); err != nil || got.Version != 2 {
		t.Fatalf("recovered node cannot serve alone: %+v %v", got, err)
	}
	all, err := c.List(ctx)
	if err != nil || len(all) != 29 {
		t.Fatalf("list after recovery %d: %v", len(all), err)
	}
}
