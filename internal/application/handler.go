package application

import (
	"encoding/json"
	"errors"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
	"io"
	"net/http"
)

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, e error) {
	status := 503
	message := "service unavailable; verify current state before retrying"
	switch {
	case errors.Is(e, domain.ErrInvalid), errors.Is(e, storage.ErrInvalid):
		status = 400
		message = "invalid input or status transition"
	case errors.Is(e, storage.ErrNotFound):
		status = 404
		message = "not found"
	case errors.Is(e, domain.ErrNoMatch):
		status = 409
		message = "no available resource meets the matching criteria"
	case errors.Is(e, storage.ErrConflict):
		status = 409
		message = "concurrent update; reload and retry"
	}
	respond(w, status, map[string]string{"error": message})
}
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	e := d.Decode(v)
	if e == nil {
		var extra any
		e = d.Decode(&extra)
		if e == io.EOF {
			return true
		}
		if e == nil {
			e = domain.ErrInvalid
		}
	}
	var large *http.MaxBytesError
	status := 400
	message := "invalid JSON body"
	if errors.As(e, &large) {
		status = 413
		message = "body too large"
	}
	respond(w, status, map[string]string{"error": message})
	return false
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /development/shards", s.shardDetails)
	mux.HandleFunc("/development/shards", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		respond(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	})
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"max_distance_meters": s.maxDistanceMeters, "distance_method": "haversine"})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.records(r.Context()); e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /requests", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Type        string          `json:"type"`
			Location    string          `json:"location"`
			Details     string          `json:"details"`
			Coordinates json.RawMessage `json:"coordinates"`
		}
		if !readBody(w, r, &in) {
			return
		}
		coordinates, e := decodeCoordinates(in.Coordinates)
		if e != nil {
			failure(w, e)
			return
		}
		out, e := s.CreateRequest(r.Context(), in.Type, in.Location, in.Details, coordinates)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("Location", "/requests/"+out.ID)
		respond(w, 201, out)
	})
	mux.HandleFunc("GET /requests", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.ListRequests(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("GET /requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.GetRequest(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("PUT /requests/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status string `json:"status"`
		}
		if !readBody(w, r, &in) {
			return
		}
		out, e := s.UpdateRequest(r.Context(), r.PathValue("id"), in.Status)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("POST /resources", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Type         string          `json:"type"`
			Location     string          `json:"location"`
			Availability *bool           `json:"availability"`
			Coordinates  json.RawMessage `json:"coordinates"`
		}
		if !readBody(w, r, &in) {
			return
		}
		if in.Availability == nil {
			failure(w, domain.ErrInvalid)
			return
		}
		coordinates, e := decodeCoordinates(in.Coordinates)
		if e != nil {
			failure(w, e)
			return
		}
		out, e := s.CreateResource(r.Context(), in.Type, in.Location, *in.Availability, coordinates)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("Location", "/resources")
		respond(w, 201, out)
	})
	mux.HandleFunc("GET /resources", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.ListResources(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("PUT /resources/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status string `json:"status"`
		}
		if !readBody(w, r, &in) {
			return
		}
		out, e := s.UpdateResource(r.Context(), r.PathValue("id"), in.Status)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("POST /match/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !validID(r.PathValue("id")) {
			failure(w, domain.ErrInvalid)
			return
		}
		out, e := s.Match(r.Context(), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { respond(w, 404, map[string]string{"error": "not found"}) })
	return mux
}
