package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func batchKeys() []string {
	keys := make([]string, 3)
	for i := 0; keys[0] == "" || keys[1] == "" || keys[2] == ""; i++ {
		key := fmt.Sprintf("batch/%d", i)
		shard, _ := storage.Shard(key, 3)
		keys[shard] = key
	}
	return keys
}
func TestBatchCrossShardUncertainRecovery(t *testing.T) {
	c, peers, _ := setup(t)
	ctx := context.Background()
	keys := batchKeys()
	peers[2].fail(true)
	peers[3].fail(true)
	mutations := []storage.Mutation{{Key: keys[0], Value: json.RawMessage(`1`)}, {Key: keys[1], Value: json.RawMessage(`2`)}, {Key: keys[2], Value: json.RawMessage(`3`)}}
	if _, err := c.Batch(ctx, mutations); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want uncertain: %v", err)
	}
	for _, key := range keys {
		if r, err := c.journal.Get(key); err != nil || r.Version != 1 {
			t.Fatalf("journal partial: %+v %v", r, err)
		}
	}
	if _, err := c.Get(ctx, keys[1]); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale copy readable: %v", err)
	}
	// Every touched shard must still be attempted after an earlier shard fails.
	if r, err := c.Get(ctx, keys[2]); err != nil || r.Version != 1 {
		t.Fatalf("later shard not replicated: %+v %v", r, err)
	}
	peers[3].fail(false)
	if err := c.Register(ctx, "s1-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.List(ctx); err != nil || len(got) != 3 {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	if _, err := c.Batch(ctx, mutations); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("retry: %v", err)
	}
}
func TestBatchPreflightRejectsNormalizedEnvelope(t *testing.T) {
	c, _, _ := setup(t)
	keys := batchKeys()
	large := json.RawMessage("[" + strings.Repeat("1e131071,", 64) + "1e131071]")
	if _, err := c.Batch(context.Background(), []storage.Mutation{{Key: keys[0], Value: json.RawMessage(`1`)}, {Key: keys[1], Value: large}}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatal(err)
	}
	if records, err := c.journal.Records(); err != nil || len(records) != 0 {
		t.Fatalf("preflight mutated: %+v %v", records, err)
	}
}
func TestBatchHTTPValidationAndAtomicCAS(t *testing.T) {
	c, _, _ := setup(t)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"mutations":[{"key":"one","value":1,"expected_version":0},{"key":"two","value":2,"expected_version":0}]}`, 200},
		{`{"mutations":[{"key":"three","value":3},{"key":"one","value":2}]}`, 409},
		{`{"mutations":[]}`, 400},
		{`{"mutations":[{"key":"x","value":1},{"key":"x","value":2}]}`, 400},
		{`{"mutations":[{"key":"one","delete":true,"expected_version":1}]}`, 200},
	} {
		r := httptest.NewRequest(http.MethodPost, "/internal/batch", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer test-secret")
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	if _, err := c.journal.Get("three"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("partial CAS write")
	}
}

func TestBatchCombinedShardEnvelopeRejected(t *testing.T) {
	c, _, _ := setup(t)
	keys := []string{}
	for i := 0; len(keys) < 2; i++ {
		key := fmt.Sprintf("combined/%d", i)
		shard, _ := storage.Shard(key, 3)
		if shard == 0 {
			keys = append(keys, key)
		}
	}
	value := json.RawMessage(`"` + strings.Repeat("x", maxBody/2) + `"`)
	_, err := c.Batch(context.Background(), []storage.Mutation{{Key: keys[0], Value: value}, {Key: keys[1], Value: value}})
	if !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("oversized combined shard: %v", err)
	}
	if records, err := c.journal.Records(); err != nil || len(records) != 0 {
		t.Fatalf("partial batch: %+v %v", records, err)
	}
}

func TestBatchBoundsAndUnavailablePreflight(t *testing.T) {
	c, _, _ := setup(t)
	for _, body := range []string{
		`{"mutations":[{"key":" ","value":1}]}`,
		`{"mutations":[{"key":"x","value":1,"expected_version":18446744073709551615}]}`,
		`{"mutations":[{"key":"x","delete":true,"value":42}]}`,
		`{"mutations":[{"key":"x","value":1,"unknown":true}]}`,
	} {
		req := httptest.NewRequest("POST", "/internal/batch", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-secret")
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
	oversized := `{"mutations":[{"key":"x","value":"` + strings.Repeat("x", maxBody) + `"}]}`
	req := httptest.NewRequest("POST", "/internal/batch", strings.NewReader(oversized))
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, req)
	if w.Code != 413 {
		t.Fatalf("body bound: %d", w.Code)
	}
	mutations := make([]storage.Mutation, storage.MaxBatchMutations+1)
	for i := range mutations {
		mutations[i] = storage.Mutation{Key: fmt.Sprintf("k%d", i), Value: json.RawMessage(`1`)}
	}
	if _, err := c.Batch(context.Background(), mutations); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("count bound: %v", err)
	}
	keys := batchKeys()
	c.nodes[2].Ready = false
	c.nodes[3].Ready = false
	if _, err := c.Batch(context.Background(), []storage.Mutation{{Key: keys[0], Value: json.RawMessage(`1`)}, {Key: keys[1], Value: json.RawMessage(`1`)}}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable: %v", err)
	}
	if records, err := c.journal.Records(); err != nil || len(records) != 0 {
		t.Fatalf("preflight changed journal: %+v %v", records, err)
	}
}
