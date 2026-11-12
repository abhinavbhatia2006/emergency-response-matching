package coordinator

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	message := "storage unavailable; read current state before retrying a write"
	switch {
	case errors.Is(err, storage.ErrInvalid):
		status = 400
		message = "invalid input"
	case errors.Is(err, storage.ErrNotFound):
		status = 404
		message = "not found"
	case errors.Is(err, storage.ErrConflict):
		status = 409
		message = "version conflict"
	}
	writeJSON(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(v)
	if err == nil {
		var extra any
		if trailingErr := decoder.Decode(&extra); trailingErr != io.EOF {
			err = trailingErr
			if err == nil {
				err = storage.ErrInvalid
			}
		}
	}
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeJSON(w, 413, map[string]string{"error": "body too large"})
		} else {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON body"})
		}
		return false
	}
	return true
}

func (c *Coordinator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /internal/nodes", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, c.Nodes()) })
	register := func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.ID == "" {
			writeError(w, storage.ErrInvalid)
			return
		}
		if err := c.Register(r.Context(), input.ID); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	}
	mux.HandleFunc("POST /internal/nodes/register", register)
	mux.HandleFunc("POST /internal/nodes/heartbeat", register)
	mux.HandleFunc("GET /internal/data", func(w http.ResponseWriter, r *http.Request) {
		records, err := c.List(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, records)
	})
	mux.HandleFunc("GET /internal/data/{key...}", func(w http.ResponseWriter, r *http.Request) {
		record, err := c.Get(r.Context(), r.PathValue("key"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, record)
	})
	mux.HandleFunc("PUT /internal/data/{key...}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Value    json.RawMessage `json:"value"`
			Expected uint64          `json:"expected_version"`
		}
		if !decode(w, r, &input) {
			return
		}
		record, err := c.Put(r.Context(), r.PathValue("key"), input.Value, input.Expected)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, record)
	})
	mux.HandleFunc("DELETE /internal/data/{key...}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Expected uint64 `json:"expected_version"`
		}
		if !decode(w, r, &input) {
			return
		}
		record, err := c.Delete(r.Context(), r.PathValue("key"), input.Expected)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, record)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.token)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}
