package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/testdb"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func serviceTest(t *testing.T) *Service {
	t.Helper()
	store, err := storage.Open(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/data", func(w http.ResponseWriter, r *http.Request) {
		rs, e := store.Records()
		if e != nil {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(rs)
	})
	mux.HandleFunc("GET /internal/data/{key...}", func(w http.ResponseWriter, r *http.Request) {
		rec, e := store.Get(r.PathValue("key"))
		if e != nil {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(rec)
	})
	mux.HandleFunc("POST /internal/batch", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Mutations []storage.Mutation `json:"mutations"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		rs, e := store.Apply(in.Mutations)
		if e != nil {
			if errors.Is(e, storage.ErrNotFound) {
				w.WriteHeader(404)
			} else if errors.Is(e, storage.ErrConflict) {
				w.WriteHeader(409)
			} else {
				w.WriteHeader(400)
			}
			return
		}
		json.NewEncoder(w).Encode(rs)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	s, err := New(server.URL, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestRequestOutboxAndLifecycle(t *testing.T) {
	s := serviceTest(t)
	ctx := context.Background()
	req, e := s.CreateRequest(ctx, " Water ", " Delhi ", "clean water")
	if e != nil {
		t.Fatal(e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 1 || jobs[0].RequestID != req.ID {
		t.Fatalf("jobs=%v err=%v", jobs, e)
	}
	if _, e = s.UpdateRequest(ctx, req.ID, "matched"); !errors.Is(e, domain.ErrInvalid) {
		t.Fatalf("direct assignment: %v", e)
	}
	if _, e = s.Match(ctx, req.ID); !errors.Is(e, domain.ErrNoMatch) {
		t.Fatalf("no match: %v", e)
	}
	resource, e := s.CreateResource(ctx, "water", "delhi", true)
	if e != nil {
		t.Fatal(e)
	}
	matched, e := s.Match(ctx, req.ID)
	if e != nil || matched.ResourceID != resource.ID || matched.Status != domain.Matched {
		t.Fatalf("match=%+v %v", matched, e)
	}
	if _, e = s.UpdateResource(ctx, resource.ID, "available"); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("reserved resource released manually")
	}
	again, e := s.Match(ctx, req.ID)
	if e != nil || again.ResourceID != resource.ID {
		t.Fatal("duplicate changed match")
	}
	if _, e = s.UpdateRequest(ctx, req.ID, "cancelled"); e != nil {
		t.Fatal(e)
	}
	resources, e := s.ListResources(ctx)
	if e != nil || !resources[0].Availability || resources[0].Status != domain.Available {
		t.Fatalf("not released: %v %v", resources, e)
	}
	if e = s.AckJob(ctx, jobs[0]); e != nil {
		t.Fatal(e)
	}
	if e = s.AckJob(ctx, jobs[0]); e != nil {
		t.Fatal("duplicate ack", e)
	}
}
func TestConcurrentMatchingNeverDoubleBooks(t *testing.T) {
	s := serviceTest(t)
	ctx := context.Background()
	if _, e := s.CreateResource(ctx, "food", "pune", true); e != nil {
		t.Fatal(e)
	}
	var requests []domain.Request
	for i := 0; i < 12; i++ {
		r, e := s.CreateRequest(ctx, "food", "pune", "meal")
		if e != nil {
			t.Fatal(e)
		}
		requests = append(requests, r)
	}
	var wg sync.WaitGroup
	for _, r := range requests {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, e := s.Match(ctx, id)
			if e != nil && !errors.Is(e, domain.ErrNoMatch) {
				t.Error(e)
			}
		}(r.ID)
	}
	wg.Wait()
	all, e := s.ListRequests(ctx)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, r := range all {
		if r.Status == domain.Matched {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("matched=%d want1", n)
	}
}
func TestCompletionConsumesResource(t *testing.T) {
	s := serviceTest(t)
	ctx := context.Background()
	s.CreateResource(ctx, "shelter", "mumbai", true)
	r, _ := s.CreateRequest(ctx, "shelter", "mumbai", "bed")
	if _, e := s.Match(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.UpdateRequest(ctx, r.ID, "completed"); e != nil {
		t.Fatal(e)
	}
	rs, e := s.ListResources(ctx)
	if e != nil || rs[0].Availability || rs[0].Status != domain.Unavailable {
		t.Fatal(rs, e)
	}
}
func TestResourceWakeupExceedsBatchCountSafely(t *testing.T) {
	s := serviceTest(t)
	ctx := context.Background()
	for i := 0; i < 260; i++ {
		if _, e := s.CreateRequest(ctx, "food", "pune", "meal"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.CreateResource(ctx, "food", "pune", true); e != nil {
		t.Fatalf("resource with many waiting requests: %v", e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil {
		t.Fatal(e)
	}
	wake := false
	for _, j := range jobs {
		if j.RequestID == "rescan" {
			wake = true
		}
	}
	if !wake {
		t.Fatal("no durable rescan job")
	}
	if _, e := s.Match(ctx, "rescan"); e != nil {
		t.Fatal(e)
	}
	all, e := s.ListRequests(ctx)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, r := range all {
		if r.Status == domain.Matched {
			n++
		}
	}
	if n != 1 {
		t.Fatal("wake matched", n)
	}
}
