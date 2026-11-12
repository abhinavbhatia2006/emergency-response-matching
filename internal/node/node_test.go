package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

func testNode(t *testing.T) (*Node, *storage.Store) {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(s, 0, 3, "secret")
	if err != nil {
		t.Fatal(err)
	}
	return n, s
}
func request(n *Node, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	n.Handler().ServeHTTP(w, r)
	return w
}
func TestNodeConfiguration(t *testing.T) {
	_, s := testNode(t)
	for _, tc := range []struct {
		s            *storage.Store
		shard, count int
		token        string
	}{{nil, 0, 3, "secret"}, {s, -1, 3, "secret"}, {s, 3, 3, "secret"}, {s, 0, 0, "secret"}, {s, 0, 2, "secret"}, {s, 0, 3, ""}, {s, 0, 3, " "}} {
		if _, err := New(tc.s, tc.shard, tc.count, tc.token); err == nil {
			t.Fatalf("accepted invalid configuration %+v", tc)
		}
	}
}
func TestNodeSnapshotAndReads(t *testing.T) {
	n, s := testNode(t)
	body := `[{"key":"hello","value":{"ok":true},"version":7}]`
	if w := request(n, "PUT", "/internal/snapshot", body, "secret"); w.Code != 200 {
		t.Fatalf("replace %d %s", w.Code, w.Body)
	}
	w := request(n, "GET", "/internal/data/hello", "", "secret")
	var got storage.Record
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 || got.Version != 7 || string(got.Value) != `{"ok":true}` {
		t.Fatalf("read %d %+v %v", w.Code, got, err)
	}
	w = request(n, "GET", "/internal/snapshot", "", "secret")
	var records []storage.Record
	if err := json.Unmarshal(w.Body.Bytes(), &records); err != nil || len(records) != 1 {
		t.Fatalf("snapshot: %s", w.Body)
	}
	if w := request(n, "PUT", "/internal/snapshot", `[{"key":"hello","version":8,"deleted":true}]`, "secret"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := request(n, "GET", "/internal/data/hello", "", "secret"); w.Code != 404 {
		t.Fatalf("tombstone readable: %d", w.Code)
	}
	if r := s.Snapshot(); len(r) != 1 || !r[0].Deleted || r[0].Version != 8 {
		t.Fatal(r)
	}
}
func TestNodeAuthAndStrictJSON(t *testing.T) {
	n, s := testNode(t)
	if w := request(n, "GET", "/healthz", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/internal/snapshot", "/internal/data/hello"} {
		for _, token := range []string{"", "wrong"} {
			if w := request(n, "GET", path, "", token); w.Code != 401 {
				t.Fatalf("unauthorized %s %d", path, w.Code)
			}
		}
	}
	for _, body := range []string{`null`, `{}`, `[] []`, `[{"key":"hello","version":1,"deleted":true,"extra":1}]`, `[{"key":"a","version":1,"deleted":true}]`, `[{"key":"hello","version":0,"deleted":true}]`, `[{`, strings.Repeat(" ", 8*1024*1024) + `[]`} {
		w := request(n, "PUT", "/internal/snapshot", body, "secret")
		if w.Code != 400 {
			t.Fatalf("invalid snapshot accepted: %d", w.Code)
		}
		if !json.Valid(w.Body.Bytes()) {
			t.Fatalf("non-JSON error: %s", w.Body)
		}
		if len(s.Snapshot()) != 0 {
			t.Fatal("invalid snapshot changed store")
		}
	}
}
func TestHeartbeatRegistrationRetryAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	paths := make(chan string, 10)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Method != "POST" {
			t.Errorf("invalid heartbeat authorization/method")
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID != "node-0" {
			t.Errorf("invalid registration body")
		}
		paths <- r.URL.Path
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/internal/nodes/heartbeat" {
			cancel()
		}
	}))
	defer server.Close()
	err := RunHeartbeat(ctx, server.Client(), server.URL, "node-0", "secret", 5*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, want := range []string{"/internal/nodes/register", "/internal/nodes/register", "/internal/nodes/heartbeat"} {
		select {
		case got := <-paths:
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
		default:
			t.Fatal("missing heartbeat")
		}
	}
}
func TestHeartbeatRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		url, id, token string
		interval       time.Duration
	}{{"invalid", "id", "token", time.Second}, {"http://localhost", "", "token", time.Second}, {"http://localhost", "id", "", time.Second}, {"http://localhost", "id", "token", 0}} {
		if err := RunHeartbeat(context.Background(), &http.Client{Timeout: time.Second}, tc.url, tc.id, tc.token, tc.interval); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestRejectTokenWithHeaderNewlines(t *testing.T) {
	_, s := testNode(t)
	if _, err := New(s, 0, 3, "secret\n"); err == nil {
		t.Fatal("accepted token that cannot be sent as an HTTP header")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunHeartbeat(ctx, &http.Client{}, "http://localhost", "node", "secret\r", time.Second); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("invalid token not rejected before network loop: %v", err)
	}
}
