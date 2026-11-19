package application

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
)

type apiTestTransport func(*http.Request) (*http.Response, error)

func (transport apiTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func apiResponse(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}

func requireAPIResponse(t *testing.T, response *httptest.ResponseRecorder, status int, target any) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body)
	}
	if response.Header().Get("Content-Type") != "application/json" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected response headers: %v", response.Header())
	}
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedAPIWritesLeaveRecordsUntouched(t *testing.T) {
	resource := geoResource("00000000000000000000000000000002", "district-a", 0)
	service, writes := geoFixture(t, []domain.Resource{resource})
	handler := service.Handler()
	requestURL := "/requests/" + geoRequestID
	resourceStatusURL := "/resources/" + resource.ID + "/status"
	unknownID := strings.Repeat("f", 32)
	cases := []struct {
		name, method, path, body string
		status                   int
	}{
		{"array creation", "POST", "/requests", `[]`, 400},
		{"legacy directions without a district", "POST", "/requests", `{"type":"food","directions":"Gate 2","details":"meal"}`, 400},
		{"unknown coordinate member", "POST", "/resources", `{"type":"food","availability":true,"coordinates":{"latitude":0,"longitude":0,"accuracy":10}}`, 400},
		{"trailing status value", "PUT", requestURL + "/status", `{"status":"cancelled"} null`, 400},
		{"non-string status", "PUT", resourceStatusURL, `{"status":true}`, 400},
		{"manual reservation", "PUT", resourceStatusURL, `{"status":"reserved"}`, 400},
		{"complete pending request", "PUT", requestURL + "/status", `{"status":"completed"}`, 400},
		{"finish missing request", "PUT", "/requests/" + unknownID + "/status", `{"status":"cancelled"}`, 404},
		{"update missing resource", "PUT", "/resources/" + unknownID + "/status", `{"status":"available"}`, 404},
		{"match missing request", "POST", "/match/" + unknownID, "", 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var problem map[string]string
			requireAPIResponse(t, apiResponse(handler, tc.method, tc.path, tc.body), tc.status, &problem)
			if problem["error"] == "" {
				t.Fatalf("missing API error: %v", problem)
			}
			if len(*writes) != 0 {
				t.Fatalf("rejected operation wrote %d records", len(*writes))
			}
		})
	}
	var request domain.Request
	requireAPIResponse(t, apiResponse(handler, "GET", requestURL, ""), 200, &request)
	if request.Status != domain.Pending || request.ResourceID != "" {
		t.Fatalf("request changed after rejected operations: %+v", request)
	}
	var resources []domain.Resource
	requireAPIResponse(t, apiResponse(handler, "GET", "/resources", ""), 200, &resources)
	if len(resources) != 1 || resources[0].ID != resource.ID ||
		resources[0].Status != domain.Available || !resources[0].Availability {
		t.Fatalf("resource changed after rejected operations: %+v", resources)
	}
}

func TestCompetingTerminalAPIUpdatesKeepResourceConsistent(t *testing.T) {
	service := serviceTest(t)
	handler := service.Handler()
	var request domain.Request
	requireAPIResponse(t, apiResponse(handler, "POST", "/requests",
		`{"type":"food","location":"district-a","details":"meal"}`), 201, &request)
	var resource domain.Resource
	requireAPIResponse(t, apiResponse(handler, "POST", "/resources",
		`{"type":"food","location":"district-a","availability":true}`), 201, &resource)
	requireAPIResponse(t, apiResponse(handler, "POST", "/match/"+request.ID, ""), 200, &request)
	if request.Status != domain.Matched || request.ResourceID != resource.ID {
		t.Fatalf("setup did not match the test resource: %+v", request)
	}

	// Let both updates read the matched version before either can write.
	var writes, conflicts atomic.Int32
	bothReady := make(chan struct{})
	service.client.Transport = apiTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/internal/batch" {
			switch writes.Add(1) {
			case 1:
				select {
				case <-bothReady:
				case <-time.After(10 * time.Second):
					return nil, errors.New("second terminal write never arrived")
				}
			case 2:
				close(bothReady)
			}
		}
		response, err := http.DefaultTransport.RoundTrip(request)
		if err == nil && response.StatusCode == http.StatusConflict {
			conflicts.Add(1)
		}
		return response, err
	})

	type result struct {
		status   domain.RequestStatus
		response *httptest.ResponseRecorder
	}
	statusURL := "/requests/" + request.ID + "/status"
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, status := range []domain.RequestStatus{domain.Completed, domain.Cancelled} {
		go func(status domain.RequestStatus) {
			<-start
			response := apiResponse(handler, "PUT", statusURL, `{"status":"`+string(status)+`"}`)
			results <- result{status, response}
		}(status)
	}
	close(start)
	var winner domain.RequestStatus
	for range 2 {
		result := <-results
		switch result.response.Code {
		case http.StatusOK:
			if winner != "" {
				t.Fatal("both incompatible terminal updates succeeded")
			}
			winner = result.status
		case http.StatusBadRequest:
		default:
			t.Fatalf("unexpected status for %s: %d %s", result.status, result.response.Code, result.response.Body)
		}
	}
	if winner == "" {
		t.Fatal("neither terminal update succeeded")
	}
	if conflicts.Load() != 1 {
		t.Fatalf("storage conflicts = %d, want one stale update", conflicts.Load())
	}
	requireAPIResponse(t, apiResponse(handler, "GET", "/requests/"+request.ID, ""), 200, &request)
	if request.Status != winner {
		t.Fatalf("final request status = %s, want %s", request.Status, winner)
	}
	var resources []domain.Resource
	requireAPIResponse(t, apiResponse(handler, "GET", "/resources", ""), 200, &resources)
	wantStatus, wantAvailability := domain.Unavailable, false
	if winner == domain.Cancelled {
		wantStatus, wantAvailability = domain.Available, true
	}
	if len(resources) != 1 || resources[0].ID != resource.ID ||
		resources[0].Status != wantStatus || resources[0].Availability != wantAvailability {
		t.Fatalf("resource after %s = %+v, want %s and availability %t", winner, resources, wantStatus, wantAvailability)
	}
	requireAPIResponse(t, apiResponse(handler, "PUT", statusURL, `{"status":"`+string(winner)+`"}`), 200, &request)
	if request.Status != winner || writes.Load() != 2 {
		t.Fatalf("terminal replay changed state: status %s, batch calls %d", request.Status, writes.Load())
	}
}
