package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/testdb"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

// A real HTTP peer that can fail independently of coordinator state.
type testPeer struct {
	mu      sync.Mutex
	records map[string]storage.Record
	failed  bool
	server  *httptest.Server
}

func newPeer(t *testing.T) *testPeer {
	p := &testPeer{records: map[string]storage.Record{}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			w.WriteHeader(401)
			return
		}
		if p.failed {
			w.WriteHeader(503)
			return
		}
		if r.Method == "PUT" && r.URL.Path == "/internal/records" {
			var records []storage.Record
			if err := json.NewDecoder(r.Body).Decode(&records); err != nil {
				w.WriteHeader(400)
				return
			}
			p.records = map[string]storage.Record{}
			for _, record := range records {
				p.records[record.Key] = record
			}
			fmt.Fprint(w, `{}`)
			return
		}
		key := r.URL.Path[len("/internal/data/"):]
		record, ok := p.records[key]
		if !ok || record.Deleted {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(record)
	}))
	t.Cleanup(p.server.Close)
	return p
}
func (p *testPeer) fail(value bool) { p.mu.Lock(); defer p.mu.Unlock(); p.failed = value }
func setup(t *testing.T) (*Coordinator, []*testPeer, string) {
	t.Helper()
	path := testdb.DSN(t)
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	var peers []*testPeer
	var nodes []NodeConfig
	for shard := 0; shard < 3; shard++ {
		for copy := 0; copy < 2; copy++ {
			p := newPeer(t)
			peers = append(peers, p)
			nodes = append(nodes, NodeConfig{ID: fmt.Sprintf("s%d-%d", shard, copy), URL: p.server.URL, Shard: shard})
		}
	}
	c, err := New(s, nodes, "test-secret", time.Minute, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if err := c.Register(context.Background(), n.ID); err != nil {
			t.Fatal(err)
		}
	}
	return c, peers, path
}

func TestReplicationFailoverAndRecovery(t *testing.T) {
	c, peers, _ := setup(t)
	ctx := context.Background()
	key := "request/flood"
	shard, _ := storage.Shard(key, 3)
	r, err := c.Put(ctx, key, json.RawMessage(`{"status":"pending"}`), 0)
	if err != nil || r.Version != 1 {
		t.Fatalf("create %+v %v", r, err)
	}
	primary, replica := peers[shard*2], peers[shard*2+1]
	for _, p := range []*testPeer{primary, replica} {
		p.mu.Lock()
		got := p.records[key]
		p.mu.Unlock()
		if got.Version != 1 {
			t.Fatal("not replicated")
		}
	}
	primary.fail(true)
	if got, err := c.Get(ctx, key); err != nil || got.Version != 1 {
		t.Fatalf("failover %+v %v", got, err)
	}
	if _, err := c.Put(ctx, key, json.RawMessage(`{"status":"matched"}`), 1); err != nil {
		t.Fatal(err)
	}
	primary.fail(false)
	if err := c.Register(ctx, fmt.Sprintf("s%d-0", shard)); err != nil {
		t.Fatal(err)
	}
	primary.mu.Lock()
	got := primary.records[key]
	primary.mu.Unlock()
	if got.Version != 2 {
		t.Fatal("recovery did not repair stale version")
	}
	if _, err := c.Delete(ctx, key, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted record visible: %v", err)
	}
	primary.mu.Lock()
	got = primary.records[key]
	primary.mu.Unlock()
	if !got.Deleted || got.Version != 3 {
		t.Fatal("deletion not replicated")
	}
}

func TestUnavailableAndHeartbeatExpiry(t *testing.T) {
	c, peers, _ := setup(t)
	ctx := context.Background()
	key := "request/expiry"
	shard, _ := storage.Shard(key, 3)
	if _, err := c.Put(ctx, key, json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	peers[shard*2].fail(true)
	peers[shard*2+1].fail(true)
	if _, err := c.Get(ctx, key); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := c.Put(ctx, key, json.RawMessage(`2`), 1); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if r, _ := c.journal.Get(key); r.Version != 1 {
		t.Fatal("unavailable write changed journal")
	}
	c.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	for _, n := range c.Nodes() {
		if n.Ready {
			t.Fatal("expired node remains ready")
		}
	}
	if err := c.Register(ctx, "unknown"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentCASAndRestart(t *testing.T) {
	c, _, path := setup(t)
	ctx := context.Background()
	key := "request/cas"
	if _, err := c.Put(ctx, key, json.RawMessage(`0`), 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := c.Put(ctx, key, json.RawMessage(`1`), 1); results <- err }()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, storage.ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d CAS winners", wins)
	}
	journal, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(journal.Close)
	var configs []NodeConfig
	for _, n := range c.Nodes() {
		configs = append(configs, n.NodeConfig)
	}
	restarted, err := New(journal, configs, "test-secret", time.Minute, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Get(ctx, key); !errors.Is(err, ErrUnavailable) {
		t.Fatal("restart served unregistered replicas")
	}
	for _, n := range configs {
		if err := restarted.Register(ctx, n.ID); err != nil {
			t.Fatal(err)
		}
	}
	r, err := restarted.Get(ctx, key)
	if err != nil || r.Version != 2 {
		t.Fatalf("restart %+v %v", r, err)
	}
}

func TestRejectStaleReplicaAndListAcrossShards(t *testing.T) {
	c, peers, _ := setup(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		if _, err := c.Put(ctx, fmt.Sprintf("request/%d", i), json.RawMessage(`{"x":1}`), 0); err != nil {
			t.Fatal(err)
		}
	}
	all, err := c.List(ctx)
	if err != nil || len(all) != 12 {
		t.Fatalf("list %d %v", len(all), err)
	}
	key := "request/0"
	shard, _ := storage.Shard(key, 3)
	p := peers[shard*2]
	p.mu.Lock()
	p.records[key] = storage.Record{Key: key, Version: 1, Value: json.RawMessage(`{"x":2}`)}
	p.mu.Unlock()
	r, err := c.Get(ctx, key)
	if err != nil || !sameJSON(r.Value, json.RawMessage(`{"x":1}`)) {
		t.Fatalf("stale replica served: %+v %v", r, err)
	}
	for _, n := range c.Nodes() {
		if n.ID == fmt.Sprintf("s%d-0", shard) && n.Ready {
			t.Fatal("corrupt replica still ready")
		}
	}
}

func TestConfigurationValidation(t *testing.T) {
	c, _, _ := setup(t)
	var configs []NodeConfig
	for _, n := range c.Nodes() {
		configs = append(configs, n.NodeConfig)
	}
	for _, mutate := range []func([]NodeConfig) []NodeConfig{
		func(n []NodeConfig) []NodeConfig { return n[:5] },
		func(n []NodeConfig) []NodeConfig { n[1].ID = n[0].ID; return n },
		func(n []NodeConfig) []NodeConfig { n[1].URL = n[0].URL; return n },
		func(n []NodeConfig) []NodeConfig { n[0].Shard = 3; return n },
		func(n []NodeConfig) []NodeConfig { n[0].URL = "file:///tmp/data"; return n },
	} {
		n := append([]NodeConfig(nil), configs...)
		if _, err := New(c.journal, mutate(n), "test-secret", time.Minute, nil); err == nil {
			t.Fatal("bad config accepted")
		}
	}
	if _, err := New(c.journal, configs, "", time.Minute, nil); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestUncertainWriteRecoveredFromJournal(t *testing.T) {
	c, peers, _ := setup(t)
	ctx := context.Background()
	key := "request/uncertain"
	shard, _ := storage.Shard(key, 3)
	// Leases still claim readiness, but both peers fail before acknowledgement.
	peers[2*shard].fail(true)
	peers[2*shard+1].fail(true)
	if _, err := c.Put(ctx, key, json.RawMessage(`{"status":"pending"}`), 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected uncertain outcome: %v", err)
	}
	committed, err := c.journal.Get(key)
	if err != nil || committed.Version != 1 {
		t.Fatalf("lost recovery intent: %+v %v", committed, err)
	}
	peers[2*shard+1].fail(false)
	if err := c.Register(ctx, fmt.Sprintf("s%d-1", shard)); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(ctx, key)
	if err != nil || got.Version != 1 {
		t.Fatalf("recovery failed: %+v %v", got, err)
	}
	if _, err := c.Put(ctx, key, json.RawMessage(`{}`), 0); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale retry changed recovered write: %v", err)
	}
}

func TestReadAfterJSONWireEscaping(t *testing.T) {
	c, _, _ := setup(t)
	ctx := context.Background()
	value := json.RawMessage("{\"details\":\"A & B <shelter>\u2028next\u2029line\",\"large\":9007199254740993}")
	if _, err := c.Put(ctx, "request/escaping", value, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "request/escaping"); err != nil {
		t.Fatalf("valid replica rejected after wire escaping: %v", err)
	}
	if records, err := c.List(ctx); err != nil || len(records) != 1 {
		t.Fatalf("list: %v %v", records, err)
	}
}

func TestReplicaComparisonPreservesLargeIntegers(t *testing.T) {
	if sameJSON(json.RawMessage(`{"id":9007199254740992}`), json.RawMessage(`{"id":9007199254740993}`)) {
		t.Fatal("distinct large IDs compare equal")
	}
	if !sameJSON(json.RawMessage(`{"a":"&","b":1}`), json.RawMessage(`{"b":1,"a":"\u0026"}`)) {
		t.Fatal("equivalent serialized objects differ")
	}
}

func TestReplicaComparisonAcceptsEquivalentNumbers(t *testing.T) {
	if !sameJSON(json.RawMessage(`{"n":[1e3,1.00,-0]}`), json.RawMessage(`{"n":[1000,1,0.0]}`)) {
		t.Fatal("equivalent JSONB numeric values rejected")
	}
}

func TestReplicaComparisonRejectsUnboundedExponents(t *testing.T) {
	for _, raw := range []string{`1e200000`, `1e-200000`} {
		if sameJSON(json.RawMessage(raw), json.RawMessage(raw)) {
			t.Fatalf("accepted exponent outside database numeric bounds: %s", raw)
		}
	}
}

func TestJournalFailureDoesNotEraseReplicasOrRenewLease(t *testing.T) {
	c, peers, _ := setup(t)
	ctx := context.Background()
	if _, err := c.Put(ctx, "hello", json.RawMessage(`1e3`), 0); err != nil {
		t.Fatal(err)
	}
	before := c.Nodes()[0].LastHeartbeat
	c.journal.Close()
	if _, err := c.List(ctx); err == nil {
		t.Fatal("database failure returned successful collection")
	}
	if err := c.Register(ctx, "s0-0"); err == nil {
		t.Fatal("registration accepted inaccessible journal")
	}
	if c.Nodes()[0].Ready || !c.Nodes()[0].LastHeartbeat.Equal(before) {
		t.Fatal("failed registration renewed readiness")
	}
	peers[0].mu.Lock()
	defer peers[0].mu.Unlock()
	if len(peers[0].records) != 1 {
		t.Fatal("database failure erased replica")
	}
}

func TestExpandedJSONRejectedBeforeJournalMutation(t *testing.T) {
	c, _, _ := setup(t)
	ctx := context.Background()
	if _, err := c.Put(ctx, "large", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	value := json.RawMessage("[" + strings.Repeat("1e131071,", 64) + "1e131071]")
	if _, err := c.Put(ctx, "large", value, 1); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("want invalid oversized JSONB, got %v", err)
	}
	got, err := c.journal.Get("large")
	if err != nil || got.Version != 1 || string(got.Value) != "1" {
		t.Fatalf("journal changed: version=%d err=%v", got.Version, err)
	}
	for _, n := range c.Nodes() {
		if err := c.Register(ctx, n.ID); err != nil {
			t.Fatal(err)
		}
	}
}
