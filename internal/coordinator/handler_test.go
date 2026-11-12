package coordinator

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPContracts(t *testing.T) {
	c, _, _ := setup(t)
	server := httptest.NewServer(c.Handler())
	defer server.Close()
	for _, tc := range []struct {
		method, path, body, token string
		want                      int
	}{
		{"GET", "/healthz", "", "", 200},
		{"GET", "/internal/nodes", "", "", 401},
		{"GET", "/internal/nodes", "", "test-secret", 200},
		{"POST", "/internal/nodes/register", `{"id":"unknown"}`, "test-secret", 404},
		{"POST", "/internal/nodes/register", `{"id":"s0-0","url":"http://other"}`, "test-secret", 400},
		{"POST", "/internal/nodes/heartbeat", `{"id":"s0-0"}`, "test-secret", 200},
		{"PUT", "/internal/data/request/1", `{"value":{"type":"water"},"expected_version":0}`, "test-secret", 200},
		{"GET", "/internal/data/request/1", "", "test-secret", 200},
		{"PUT", "/internal/data/request/1", `{"value":{},"expected_version":0}`, "test-secret", 409},
		{"PUT", "/internal/data/request/2", `{"value":{}} {}`, "test-secret", 400},
		{"PUT", "/internal/data/request/2", `{"expected_version":0}`, "test-secret", 400},
		{"PUT", "/internal/data/request/2", `{"value":{},"extra":true}`, "test-secret", 400},
		{"GET", "/internal/data", "", "test-secret", 200},
		{"DELETE", "/internal/data/request/1", `{"expected_version":1}`, "test-secret", 200},
		{"GET", "/internal/data/request/1", "", "test-secret", 404},
		{"GET", "/internal/unknown", "", "test-secret", 404},
	} {
		req, _ := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s: got %d want %d: %s", tc.method, tc.path, resp.StatusCode, tc.want, body)
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Errorf("non-JSON response %s", tc.path)
		}
	}
}

func TestOversizedTrailingJSONWhitespace(t *testing.T) {
	c, _, _ := setup(t)
	req := httptest.NewRequest("POST", "/internal/nodes/register", strings.NewReader(`{"id":"s0-0"}`+strings.Repeat(" ", maxBody)))
	req.Header.Set("Authorization", "Bearer test-secret")
	response := httptest.NewRecorder()
	c.Handler().ServeHTTP(response, req)
	if response.Code != 413 {
		t.Fatalf("got %d, want 413", response.Code)
	}
}
