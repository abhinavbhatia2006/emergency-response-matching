package domain

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCoordinatesValidate(t *testing.T) {
	for _, coordinates := range []Coordinates{{0, 0}, {90, 180}, {-90, -180}, {28.6139, 77.2090}} {
		if err := coordinates.Validate(); err != nil {
			t.Errorf("valid coordinates %+v: %v", coordinates, err)
		}
	}
	for _, coordinates := range []Coordinates{{90.00001, 0}, {-90.00001, 0}, {0, 180.00001}, {0, -180.00001}, {math.NaN(), 0}, {0, math.NaN()}, {math.Inf(1), 0}, {math.Inf(-1), 0}, {0, math.Inf(1)}, {0, math.Inf(-1)}} {
		if err := coordinates.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid coordinates %+v: %v", coordinates, err)
		}
	}
}

func TestCoordinatesAndDistanceJSONRoundTrip(t *testing.T) {
	for _, coordinates := range []Coordinates{{0, 0}, {28.6139, 77.2090}, {-90, 180}} {
		for _, distance := range []float64{0, 1234.5} {
			request := Request{ID: "request", Coordinates: &coordinates, DistanceMeters: &distance}
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			var wireCoordinates map[string]float64
			if err := json.Unmarshal(fields["coordinates"], &wireCoordinates); err != nil {
				t.Fatal(err)
			}
			if len(wireCoordinates) != 2 || wireCoordinates["latitude"] != coordinates.Latitude || wireCoordinates["longitude"] != coordinates.Longitude {
				t.Fatalf("coordinate wire format: %s", raw)
			}
			if _, ok := fields["distance_meters"]; !ok {
				t.Fatalf("distance omitted: %s", raw)
			}
			var restored Request
			if err := json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored, request) {
				t.Fatalf("request roundtrip: %+v", restored)
			}
		}
		resource := Resource{ID: "resource", Coordinates: &coordinates}
		raw, err := json.Marshal(resource)
		if err != nil {
			t.Fatal(err)
		}
		var restored Resource
		if err := json.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(restored, resource) {
			t.Fatalf("resource roundtrip: %+v", restored)
		}
	}
}

func TestLegacyRecordsOmitOptionalGeographicFields(t *testing.T) {
	var request Request
	if err := json.Unmarshal([]byte(`{"id":"request","type":"water","location":"north","details":"needed","status":"pending","created_at":"2026-11-11T12:00:00Z"}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.Coordinates != nil || request.DistanceMeters != nil {
		t.Fatalf("legacy request gained geographic data: %+v", request)
	}
	var resource Resource
	if err := json.Unmarshal([]byte(`{"id":"resource","type":"water","location":"north","availability":true,"status":"available"}`), &resource); err != nil {
		t.Fatal(err)
	}
	if resource.Coordinates != nil {
		t.Fatalf("legacy resource gained coordinates: %+v", resource)
	}
	for _, value := range []any{request, resource} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"coordinates", "distance_meters"} {
			if _, ok := fields[name]; ok {
				t.Fatalf("nil field %s serialized: %s", name, raw)
			}
		}
	}
}

func TestCoordinatesDistanceMeters(t *testing.T) {
	for _, tc := range []struct {
		name            string
		from, to        Coordinates
		want, tolerance float64
	}{
		{"identical", Coordinates{28.6139, 77.2090}, Coordinates{28.6139, 77.2090}, 0, 0},
		{"one degree latitude", Coordinates{0, 0}, Coordinates{1, 0}, 111195.08, 0.01},
		{"one degree longitude", Coordinates{0, 0}, Coordinates{0, 1}, 111195.08, 0.01},
		{"antimeridian", Coordinates{0, 179.5}, Coordinates{0, -179.5}, 111195.08, 0.01},
		{"same antimeridian", Coordinates{0, 180}, Coordinates{0, -180}, 0, 0.000001},
		{"north pole longitude", Coordinates{90, 0}, Coordinates{90, 180}, 0, 0.000001},
		{"opposite poles", Coordinates{90, 0}, Coordinates{-90, 0}, 20015114.442, 0.01},
		{"equatorial antipodes", Coordinates{0, 0}, Coordinates{0, 180}, 20015114.442, 0.01},
		{"oblique antipodes", Coordinates{45, 30}, Coordinates{-45, -150}, 20015114.442, 0.3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.from.DistanceMeters(tc.to)
			if err != nil || math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-tc.want) > tc.tolerance {
				t.Fatalf("distance = %v, %v; want %v ± %v", got, err, tc.want, tc.tolerance)
			}
			reverse, err := tc.to.DistanceMeters(tc.from)
			if err != nil || math.Abs(reverse-got) > 0.000001 {
				t.Fatalf("asymmetric distance: %v vs %v, %v", got, reverse, err)
			}
		})
	}
}

func TestCoordinatesDistanceRejectsInvalidEndpoints(t *testing.T) {
	valid := Coordinates{0, 0}
	for _, invalid := range []Coordinates{{91, 0}, {-91, 0}, {0, 181}, {0, -181}, {math.NaN(), 0}, {0, math.NaN()}, {math.Inf(1), 0}, {math.Inf(-1), 0}, {0, math.Inf(1)}, {0, math.Inf(-1)}} {
		if _, err := invalid.DistanceMeters(valid); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid origin %+v accepted: %v", invalid, err)
		}
		if _, err := valid.DistanceMeters(invalid); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid destination %+v accepted: %v", invalid, err)
		}
	}
}
