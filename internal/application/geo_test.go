package application

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

const geoRequestID = "00000000000000000000000000000001"

// Exercise application behavior through its coordinator HTTP contract. Real
// PostgreSQL transactions are independently covered by service_test.go.
func geoFixture(t *testing.T, resources []domain.Resource) (*Service, *[]storage.Mutation) {
	t.Helper()
	req := domain.Request{ID: geoRequestID, Type: domain.Food, Location: "district-a", Details: "meal", Status: domain.Pending, Coordinates: &domain.Coordinates{Latitude: 0, Longitude: 0}}
	records := map[string]storage.Record{}
	add := func(key string, value any) {
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		records[key] = storage.Record{Key: key, Value: b, Version: 1}
	}
	add("request/"+req.ID, req)
	for _, resource := range resources {
		add("resource/"+resource.ID, resource)
	}
	var mu sync.Mutex
	var writes []storage.Mutation
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "GET" {
			if r.URL.Path == "/internal/data" {
				out := []storage.Record{}
				for _, record := range records {
					out = append(out, record)
				}
				json.NewEncoder(w).Encode(out)
				return
			}
			record, ok := records[strings.TrimPrefix(r.URL.Path, "/internal/data/")]
			if !ok || record.Deleted {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(record)
			return
		}
		var body struct {
			Mutations []storage.Mutation `json:"mutations"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		for _, m := range body.Mutations {
			if records[m.Key].Version != m.Expected {
				w.WriteHeader(409)
				return
			}
		}
		out := []storage.Record{}
		for _, m := range body.Mutations {
			record := storage.Record{Key: m.Key, Value: m.Value, Version: m.Expected + 1, Deleted: m.Delete}
			records[m.Key] = record
			out = append(out, record)
		}
		writes = append(writes, body.Mutations...)
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(backend.Close)
	s, e := New(backend.URL, "secret")
	if e != nil {
		t.Fatal(e)
	}
	return s, &writes
}
func geoResource(id, location string, latitude float64) domain.Resource {
	return domain.Resource{ID: id, Type: domain.Food, Location: location, Availability: true, Status: domain.Available, Coordinates: &domain.Coordinates{Latitude: latitude, Longitude: 0}}
}
func TestGeoNearestCrossDistrictAndPersistence(t *testing.T) {
	s, writes := geoFixture(t, []domain.Resource{geoResource("far", "district-a", 0.4), geoResource("near", "district-b", 0.1)})
	got, e := s.Match(context.Background(), geoRequestID)
	if e != nil || got.ResourceID != "near" {
		t.Fatalf("match=%+v err=%v", got, e)
	}
	stored, e := s.GetRequest(context.Background(), geoRequestID)
	if e != nil || stored.DistanceMeters == nil || math.Abs(*stored.DistanceMeters-11119.5) > 1 {
		t.Fatalf("persisted=%+v error=%v", stored, e)
	}
	if len(*writes) != 2 {
		t.Fatalf("atomic assignment mutations=%v", *writes)
	}
	resources, e := s.ListResources(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range resources {
		if r.ID == "near" && (r.Availability || r.Status != domain.Reserved) {
			t.Fatal("chosen resource not reserved")
		}
	}
}
func TestGeoThresholdTieZeroAndMissingCoordinates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resources []domain.Resource
		want      string
	}{
		{"under50km", []domain.Resource{geoResource("r", "other", 0.449)}, "r"},
		{"over50km", []domain.Resource{geoResource("r", "district-a", 0.450)}, ""},
		{"tie", []domain.Resource{geoResource("b", "other", 0.1), geoResource("a", "other", -0.1)}, "a"},
		{"zero", []domain.Resource{geoResource("r", "other", 0)}, "r"},
		{"no-coordinates", []domain.Resource{{ID: "legacy", Type: domain.Food, Location: "district-a", Availability: true, Status: domain.Available}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, writes := geoFixture(t, tc.resources)
			got, e := s.Match(context.Background(), geoRequestID)
			if tc.want == "" {
				if !errors.Is(e, domain.ErrNoMatch) || len(*writes) != 0 {
					t.Fatalf("error=%v writes=%v", e, *writes)
				}
				return
			}
			if e != nil || got.ResourceID != tc.want || got.DistanceMeters == nil {
				t.Fatalf("match=%+v error=%v", got, e)
			}
		})
	}
}
func TestGeoConfiguredInclusiveLimitAndConfig(t *testing.T) {
	s, _ := geoFixture(t, []domain.Resource{geoResource("r", "other", 0.1)})
	// Hand-calculated arc of 0.1 degree on the mean Earth radius (6371008.8m).
	boundary := 6371008.8 * (0.1 * math.Pi / 180)
	for _, tc := range []struct {
		limit float64
		want  int
	}{{boundary, 200}, {boundary - 0.001, 409}} {
		fresh, _ := geoFixture(t, []domain.Resource{geoResource("r", "other", 0.1)})
		configured, e := New(fresh.base, "secret", WithMaxDistanceMeters(tc.limit))
		if e != nil {
			t.Fatal(e)
		}
		w := httptest.NewRecorder()
		configured.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/match/"+geoRequestID, nil))
		if w.Code != tc.want {
			t.Fatalf("limit=%v status=%d body=%s", tc.limit, w.Code, w.Body.String())
		}
	}
	for _, limit := range []float64{0, -1, math.NaN(), math.Inf(1), 20015001} {
		if _, e := New(s.base, "secret", WithMaxDistanceMeters(limit)); !errors.Is(e, domain.ErrInvalid) {
			t.Fatalf("limit=%v error=%v", limit, e)
		}
	}
	configured, e := New(s.base, "secret", WithMaxDistanceMeters(12000))
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	configured.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/config", nil))
	var config struct {
		Max    float64 `json:"max_distance_meters"`
		Method string  `json:"distance_method"`
	}
	json.Unmarshal(w.Body.Bytes(), &config)
	if w.Code != 200 || config.Max != 12000 || config.Method != "haversine" {
		t.Fatalf("config=%s", w.Body.String())
	}
}
func TestGeoRescanAndConcurrentReservation(t *testing.T) {
	s, _ := geoFixture(t, []domain.Resource{geoResource("r", "other", 0.1)})
	if _, e := s.Match(context.Background(), "rescan"); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetRequest(context.Background(), geoRequestID)
	if e != nil || got.ResourceID != "r" {
		t.Fatalf("rescan=%+v error=%v", got, e)
	}
	s, _ = geoFixture(t, []domain.Resource{geoResource("r", "other", 0.1)})
	second, e := s.CreateRequest(context.Background(), "food", "different", "meal", &domain.Coordinates{})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for _, id := range []string{geoRequestID, second.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, e := s.Match(context.Background(), id)
			if e != nil && !errors.Is(e, domain.ErrNoMatch) {
				t.Error(e)
			}
		}(id)
	}
	wg.Wait()
	requests, e := s.ListRequests(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, r := range requests {
		if r.Status == domain.Matched {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("matched=%d", count)
	}
}
func TestGeoInvalidStoredCoordinatesFailClosed(t *testing.T) {
	s, writes := geoFixture(t, []domain.Resource{geoResource("r", "district-a", 91)})
	_, e := s.Match(context.Background(), geoRequestID)
	if !errors.Is(e, ErrUnavailable) || len(*writes) != 0 {
		t.Fatalf("error=%v writes=%v", e, *writes)
	}
}
func TestGeoCreationCopiesAndValidatesCoordinates(t *testing.T) {
	s, writes := geoFixture(t, nil)
	c := &domain.Coordinates{}
	req, e := s.CreateRequest(context.Background(), "food", "a", "meal", c)
	if e != nil {
		t.Fatal(e)
	}
	resource, e := s.CreateResource(context.Background(), "food", "b", true, c)
	if e != nil {
		t.Fatal(e)
	}
	c.Latitude = 50
	if req.Coordinates == nil || req.Coordinates.Latitude != 0 || resource.Coordinates == nil || resource.Coordinates.Latitude != 0 {
		t.Fatal("coordinates not copied")
	}
	before := len(*writes)
	for _, bad := range []*domain.Coordinates{{Latitude: 91}, {Longitude: 181}, {Latitude: math.NaN()}, {Longitude: math.Inf(1)}} {
		if _, e := s.CreateRequest(context.Background(), "food", "a", "meal", bad); !errors.Is(e, domain.ErrInvalid) {
			t.Fatal(e)
		}
		if _, e := s.CreateResource(context.Background(), "food", "a", true, bad); !errors.Is(e, domain.ErrInvalid) {
			t.Fatal(e)
		}
	}
	if len(*writes) != before {
		t.Fatal("invalid coordinates persisted")
	}
}
func TestCoordinateHTTPInput(t *testing.T) {
	for _, endpoint := range []string{"requests", "resources"} {
		for _, tc := range []struct {
			coordinates string
			status      int
		}{
			{`{"latitude":0,"longitude":0}`, 201}, {`{"latitude":90,"longitude":-180}`, 201},
			{`{"latitude":1}`, 400}, {`{"longitude":1}`, 400}, {`null`, 400},
			{`{"latitude":null,"longitude":1}`, 400}, {`{"latitude":1,"longitude":null}`, 400},
			{`{"latitude":91,"longitude":0}`, 400}, {`{"latitude":0,"longitude":181}`, 400},
			{`{"latitude":0,"longitude":0,"extra":1}`, 400},
		} {
			t.Run(endpoint+tc.coordinates, func(t *testing.T) {
				s, writes := geoFixture(t, nil)
				body := `{"type":"food","location":"district-a",`
				if endpoint == "requests" {
					body += `"details":"meal",`
				} else {
					body += `"availability":true,`
				}
				body += `"coordinates":` + tc.coordinates + `}`
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/"+endpoint, strings.NewReader(body)))
				if w.Code != tc.status {
					t.Fatalf("status=%d body=%s want=%d", w.Code, w.Body.String(), tc.status)
				}
				if tc.status == 201 {
					var out struct {
						Coordinates *domain.Coordinates `json:"coordinates"`
					}
					json.Unmarshal(w.Body.Bytes(), &out)
					if out.Coordinates == nil || len(*writes) != 2 {
						t.Fatal("coordinates or atomic job missing")
					}
				} else if len(*writes) != 0 {
					t.Fatal("invalid input persisted")
				}
			})
		}
	}
}

type geoTransportFunc func(*http.Request) (*http.Response, error)

func (f geoTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGeoCASConflictRecomputesSelectionAndDistance(t *testing.T) {
	const nearest = "00000000000000000000000000000010"
	const next = "00000000000000000000000000000020"
	s, _ := geoFixture(t, []domain.Resource{geoResource(nearest, "other", 0.1), geoResource(next, "other", 0.2)})
	other, err := New(s.base, "secret")
	if err != nil {
		t.Fatal(err)
	}
	intercepted := false
	s.client.Transport = geoTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && !intercepted {
			intercepted = true
			// A competing update lands after the matcher reads its candidates and
			// before its atomic CAS. The fixture rejects that stale CAS.
			if _, err := other.UpdateResource(r.Context(), nearest, "unavailable"); err != nil {
				return nil, err
			}
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	got, err := s.Match(context.Background(), geoRequestID)
	if err != nil || got.ResourceID != next || got.DistanceMeters == nil || math.Abs(*got.DistanceMeters-22239) > 1 {
		t.Fatalf("retry retained stale candidate/distance: match=%+v error=%v", got, err)
	}
}

func TestGeoLegacyRequestRetainsDistrictMatching(t *testing.T) {
	s, _ := geoFixture(t, []domain.Resource{geoResource("near-other", "other", 0), geoResource("far-local", "district-a", 80)})
	request, err := s.CreateRequest(context.Background(), "food", "district-a", "legacy meal")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Match(context.Background(), request.ID)
	if err != nil || got.ResourceID != "far-local" || got.DistanceMeters != nil {
		t.Fatalf("legacy match=%+v error=%v", got, err)
	}
}

func TestGeoPostgresLifecycle(t *testing.T) {
	s := serviceTest(t)
	ctx := context.Background()
	resource, err := s.CreateResource(ctx, "food", "other", true, &domain.Coordinates{Latitude: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.CreateRequest(ctx, "food", "district-a", "meal", &domain.Coordinates{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Match(ctx, "rescan"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRequest(ctx, request.ID)
	if err != nil || got.ResourceID != resource.ID || got.DistanceMeters == nil || math.Abs(*got.DistanceMeters-11119.5) > 1 {
		t.Fatalf("persisted=%+v error=%v", got, err)
	}
	if _, err := s.UpdateRequest(ctx, request.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	resources, err := s.ListResources(ctx)
	if err != nil || len(resources) != 1 || !resources[0].Availability || resources[0].Status != domain.Available {
		t.Fatalf("release=%v error=%v", resources, err)
	}
}
