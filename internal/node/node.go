// Package node serves a durable replica of one fixed shard.
package node

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

type Node struct {
	store        *storage.Store
	shard, count int
	token        string
}

func New(store *storage.Store, shard, count int, token string) (*Node, error) {
	if store == nil || count != 3 || shard < 0 || shard >= count || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, storage.ErrInvalid
	}
	return &Node{store: store, shard: shard, count: count, token: token}, nil
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func (n *Node) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			respond(w, 200, map[string]string{"status": "ok"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+n.token)) != 1 {
			fail(w, 401, "unauthorized")
			return
		}
		switch {
		case r.URL.Path == "/internal/snapshot":
			switch r.Method {
			case http.MethodGet:
				respond(w, 200, n.store.Snapshot())
			case http.MethodPut:
				r.Body = http.MaxBytesReader(w, r.Body, 8*1024*1024)
				decoder := json.NewDecoder(r.Body)
				decoder.DisallowUnknownFields()
				var records []storage.Record
				if err := decoder.Decode(&records); err != nil || records == nil {
					fail(w, 400, "invalid snapshot")
					return
				}
				var extra any
				if err := decoder.Decode(&extra); err != io.EOF {
					fail(w, 400, "invalid snapshot")
					return
				}
				for _, record := range records {
					shard, err := storage.Shard(record.Key, n.count)
					if err != nil || shard != n.shard {
						fail(w, 400, "invalid shard key")
						return
					}
				}
				if err := n.store.ReplaceSnapshot(records); err != nil {
					if errors.Is(err, storage.ErrInvalid) {
						fail(w, 400, "invalid snapshot")
					} else {
						fail(w, 503, "snapshot persistence failed")
					}
					return
				}
				respond(w, 200, map[string]string{"status": "ok"})
			default:
				fail(w, 405, "method not allowed")
			}
		case strings.HasPrefix(r.URL.Path, "/internal/data/"):
			if r.Method != http.MethodGet {
				fail(w, 405, "method not allowed")
				return
			}
			key := strings.TrimPrefix(r.URL.Path, "/internal/data/")
			shard, err := storage.Shard(key, n.count)
			if err != nil || shard != n.shard {
				fail(w, 400, "invalid shard key")
				return
			}
			record, err := n.store.Get(key)
			if err != nil {
				fail(w, 404, "record not found")
				return
			}
			respond(w, 200, record)
		default:
			fail(w, 404, "not found")
		}
	})
}

// RunHeartbeat registers until successful, then renews the coordinator lease.
// Transient failures are retried until context cancellation. The supplied client
// is copied so adding a finite default timeout never mutates caller state.
func RunHeartbeat(ctx context.Context, client *http.Client, coordinatorURL, id, token string, interval time.Duration) error {
	base, err := url.Parse(coordinatorURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.TrimSpace(id) == "" || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") || interval <= 0 || client == nil {
		return storage.ErrInvalid
	}
	transport := *client
	if transport.Timeout <= 0 {
		transport.Timeout = 5 * time.Second
	}
	// Redirects must not move the internal bearer token to a different endpoint.
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	body, _ := json.Marshal(map[string]string{"id": id})
	endpoint := "/internal/nodes/register"
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := *base
		target.Path = strings.TrimRight(base.Path, "/") + endpoint
		target.RawPath = ""
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
		if err != nil {
			return storage.ErrInvalid
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := transport.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				endpoint = "/internal/nodes/heartbeat"
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
