package application

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func shardFixture() string {
	var nodes []map[string]any
	for shard := 2; shard >= 0; shard-- {
		for _, role := range []string{"replica", "primary"} {
			nodes = append(nodes, map[string]any{"id": fmt.Sprintf("shard-%d-%s", shard, role), "shard": shard, "url": "http://private-node:9100", "ready": role == "primary", "last_heartbeat": "2026-11-18T00:00:00Z"})
		}
	}
	nodes[0]["last_heartbeat"] = "0001-01-01T00:00:00Z"
	b, _ := json.Marshal(nodes)
	return string(b)
}

func TestShardDetailsSanitizedLiveTopology(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/internal/nodes" || r.Header.Get("Authorization") != "Bearer private-token" {
			t.Errorf("unexpected upstream request")
		}
		_, _ = w.Write([]byte(shardFixture()))
	}))
	defer upstream.Close()
	s, e := New(upstream.URL, "private-token")
	if e != nil {
		t.Fatal(e)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/development/shards", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if calls != 1 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing live read or no-store")
	}
	body := rec.Body.String()
	if strings.Contains(body, "private") || strings.Contains(body, "url") {
		t.Fatal("internal information leaked", body)
	}
	var result struct {
		Shards []struct {
			ID    int `json:"id"`
			Nodes []struct {
				ID        string  `json:"id"`
				Ready     bool    `json:"ready"`
				Heartbeat *string `json:"last_heartbeat"`
			} `json:"nodes"`
		} `json:"shards"`
	}
	if e = json.Unmarshal(rec.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Shards) != 3 {
		t.Fatal(body)
	}
	for i, shard := range result.Shards {
		if shard.ID != i || len(shard.Nodes) != 2 || shard.Nodes[0].ID != fmt.Sprintf("shard-%d-primary", i) || !shard.Nodes[0].Ready || shard.Nodes[1].Ready {
			t.Fatal(body)
		}
	}
	if result.Shards[2].Nodes[1].Heartbeat != nil {
		t.Fatal("never-seen heartbeat must be null")
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/development/shards", nil))
	if rec.Code != 405 || calls != 1 {
		t.Fatal("development endpoint must be read-only")
	}
}

func TestShardDetailsUnavailable(t *testing.T) {
	for name, body := range map[string]string{"empty": "[]", "null": "null", "malformed": "{", "wrong_shape": "{}", "invalid_shard": strings.Replace(shardFixture(), `"shard":2`, `"shard":9`, 1), "missing_ready": strings.Replace(shardFixture(), `"ready":false,`, "", 1), "bad_heartbeat": strings.Replace(shardFixture(), "2026-11-18T00:00:00Z", "bad", 1), "duplicate_id": strings.Replace(shardFixture(), "shard-2-replica", "shard-2-primary", 1)} {
		t.Run(name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer upstream.Close()
			s, _ := New(upstream.URL, "private-token")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/development/shards", nil))
			if rec.Code != 503 || strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	for _, code := range []int{401, 404, 500, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte("private-token"))
			}))
			defer upstream.Close()
			s, _ := New(upstream.URL, "private-token")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/development/shards", nil))
			if rec.Code != 503 || strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
