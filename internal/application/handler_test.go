package application

import (
	"bytes"
	"encoding/json"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicAPIContract(t *testing.T) {
	s := serviceTest(t)
	h := s.Handler()
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		if w.Code != status {
			t.Fatalf("%s %s got%d %s want%d", method, path, w.Code, w.Body.String(), status)
		}
		if w.Header().Get("Content-Type") != "application/json" {
			t.Fatal("not JSON")
		}
		return w.Body.Bytes()
	}
	call("POST", "/requests", `{"type":"water","location":"delhi","details":"need water","extra":1}`, 400)
	call("POST", "/resources", `{"type":"water","location":"delhi"}`, 400)
	call("POST", "/resources", `null`, 400)
	call("POST", "/requests", `{} {}`, 400)
	var req domain.Request
	json.Unmarshal(call("POST", "/requests", `{"type":"water","location":"delhi","details":"need water"}`, 201), &req)
	call("GET", "/requests/"+req.ID, "", 200)
	call("GET", "/requests", "", 200)
	call("POST", "/match/"+req.ID, "", 409)
	var resource domain.Resource
	json.Unmarshal(call("POST", "/resources", `{"type":"water","location":"delhi","availability":true}`, 201), &resource)
	call("GET", "/resources", "", 200)
	call("PUT", "/resources/"+resource.ID+"/status", `{"status":"unavailable"}`, 200)
	call("PUT", "/resources/"+resource.ID+"/status", `{"status":"available"}`, 200)
	call("POST", "/match/"+req.ID, "", 200)
	call("PUT", "/requests/"+req.ID+"/status", `{"status":"completed"}`, 200)
	call("GET", "/requests/00000000000000000000000000000000", "", 404)
	call("GET", "/healthz", "", 200)
	call("GET", "/readyz", "", 200)
	call("GET", "/missing", "", 404)
}
func TestAPIOutageAndBodyLimit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer backend.Close()
	s, _ := New(backend.URL, "secret")
	h := s.Handler()
	for _, path := range []string{"/requests", "/resources", "/readyz"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 503 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/requests", bytes.NewReader(bytes.Repeat([]byte(" "), (64<<10)+1))))
	if w.Code != 413 {
		t.Fatal("oversize", w.Code)
	}
}
