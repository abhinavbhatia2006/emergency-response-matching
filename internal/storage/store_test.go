package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestPersistenceAndTombstones(t *testing.T) {
	s, path := openTestStore(t)
	r, err := s.Put("request/1", json.RawMessage(`{"status":"pending"}`), 0)
	if err != nil || r.Version != 1 {
		t.Fatalf("put: %+v %v", r, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get("request/1")
	if err != nil || string(got.Value) != `{"status":"pending"}` {
		t.Fatalf("restart: %+v %v", got, err)
	}
	r, err = reopened.Delete("request/1", 1)
	if err != nil || r.Version != 2 || !r.Deleted {
		t.Fatalf("delete: %+v %v", r, err)
	}
	reopened, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get("request/1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted record visible: %v", err)
	}
	snapshot := reopened.Snapshot()
	if len(snapshot) != 1 || !snapshot[0].Deleted || snapshot[0].Version != 2 {
		t.Fatalf("lost tombstone: %+v", snapshot)
	}
	if _, err := reopened.Put("request/1", json.RawMessage(`{}`), 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale create resurrected tombstone: %v", err)
	}
	if r, err = reopened.Put("request/1", json.RawMessage(`{}`), 2); err != nil || r.Version != 3 {
		t.Fatalf("versioned recreation: %+v %v", r, err)
	}
}

func TestConflictAndDetachedValues(t *testing.T) {
	s, _ := openTestStore(t)
	input := json.RawMessage(`{"a":1}`)
	created, err := s.Put("k", input, 0)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 'x'
	created.Value[0] = 'x'
	got, _ := s.Get("k")
	if string(got.Value) != `{"a":1}` {
		t.Fatal("aliased put value")
	}
	got.Value[0] = 'x'
	snap := s.Snapshot()
	snap[0].Value[0] = 'x'
	if _, err := s.Put("k", json.RawMessage(`{}`), 0); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.Delete("k", 0); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	got, _ = s.Get("k")
	if string(got.Value) != `{"a":1}` || got.Version != 1 {
		t.Fatalf("state changed: %+v", got)
	}
	if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Delete("missing", 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentCompareAndSwap(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.Put("k", json.RawMessage(`0`), 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Put("k", json.RawMessage(`1`), 1); results <- err }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("%d concurrent winners", successes)
	}
	got, _ := s.Get("k")
	if got.Version != 2 {
		t.Fatal(got.Version)
	}
}

func TestRejectInvalidAndCorruptData(t *testing.T) {
	s, _ := openTestStore(t)
	for _, tc := range []struct{ key, value string }{{"", "{}"}, {" ", "{}"}, {"k", "{"}, {"k", ""}} {
		if _, err := s.Put(tc.key, json.RawMessage(tc.value), 0); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid %q/%q accepted: %v", tc.key, tc.value, err)
		}
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	for _, content := range []string{`garbage`, `null`, `{"k":{"key":"other","version":1,"value":{}}}`, `{"k":{"key":"k","version":0,"value":{}}}`} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil {
			t.Fatalf("accepted corrupt snapshot %s", content)
		}
	}
}

func TestFailedPersistenceDoesNotChangeMemory(t *testing.T) {
	s, path := openTestStore(t)
	if _, err := s.Put("k", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	// Replace the target file with a directory so atomic rename cannot succeed.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("k", json.RawMessage(`2`), 1); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	got, err := s.Get("k")
	if err != nil || got.Version != 1 || string(got.Value) != `1` {
		t.Fatalf("failed write leaked: %+v %v", got, err)
	}
}

func TestSnapshotStableOrdering(t *testing.T) {
	s, _ := openTestStore(t)
	for _, k := range []string{"z", "a", "m"} {
		if _, err := s.Put(k, json.RawMessage(`{}`), 0); err != nil {
			t.Fatal(err)
		}
	}
	snap := s.Snapshot()
	if fmt.Sprint(snap[0].Key, snap[1].Key, snap[2].Key) != "amz" {
		t.Fatalf("unordered snapshot: %+v", snap)
	}
}
