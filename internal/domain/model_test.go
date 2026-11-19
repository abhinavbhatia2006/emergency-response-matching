package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateRequest(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("IST", 19800))
	ids := map[string]bool{}
	for _, kind := range []string{"food", "water", "medicine", "transportation", "shelter"} {
		r, err := NewRequest(" "+strings.ToUpper(kind)+" ", LocationInput{Label: " North District ", Coordinates: &Coordinates{}}, " Drinking water needed ", now)
		if err != nil {
			t.Fatal(err)
		}
		if r.ID == "" || ids[r.ID] {
			t.Fatalf("invalid or repeated ID %q", r.ID)
		}
		ids[r.ID] = true
		if string(r.Type) != kind || r.Location != "north district" || r.Details != "Drinking water needed" || r.Status != Pending || !r.CreatedAt.Equal(now) || r.CreatedAt.Location() != time.UTC || r.ResourceID != "" {
			t.Fatalf("unexpected request: %+v", r)
		}
	}
}

func TestRejectInvalidRequest(t *testing.T) {
	for _, tc := range []struct {
		kind, location, details string
		now                     time.Time
	}{
		{"fuel", "north", "needed", time.Now()},
		{"water", "north", " ", time.Now()}, {"water", "north", "needed", time.Time{}},
		{"water", strings.Repeat("x", 201), "needed", time.Now()}, {"water", "north", strings.Repeat("x", 4001), time.Now()},
	} {
		if _, err := NewRequest(tc.kind, LocationInput{Label: tc.location, Coordinates: &Coordinates{}}, tc.details, tc.now); !errors.Is(err, ErrInvalid) {
			t.Errorf("expected invalid input, got %v", err)
		}
	}
}

func TestResourceAvailability(t *testing.T) {
	for _, available := range []bool{true, false} {
		r, err := NewResource(" WATER ", LocationInput{Label: " North ", Coordinates: &Coordinates{}}, available)
		if err != nil {
			t.Fatal(err)
		}
		want := Unavailable
		if available {
			want = Available
		}
		if r.ID == "" || r.Type != Water || r.Location != "north" || r.Availability != available || r.Status != want {
			t.Fatalf("unexpected resource: %+v", r)
		}
	}
	if _, err := NewResource("fuel", LocationInput{Coordinates: &Coordinates{}}, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestRejectInvalidUTF8Location(t *testing.T) {
	for _, location := range []string{"\xff", "\xfe"} {
		input := LocationInput{Label: location, Coordinates: &Coordinates{}}
		if _, err := NewRequest("water", input, "needed", time.Now()); !errors.Is(err, ErrInvalid) {
			t.Errorf("request accepted malformed location: %v", err)
		}
		if _, err := NewResource("water", input, true); !errors.Is(err, ErrInvalid) {
			t.Errorf("resource accepted malformed location: %v", err)
		}
	}
}

func TestRequestTransitions(t *testing.T) {
	for _, tc := range []struct {
		from, to RequestStatus
		assigned string
		ok       bool
	}{
		{Pending, Matched, "resource-1", true}, {Pending, Matched, "", false},
		{Pending, Cancelled, "", true}, {Matched, Completed, "resource-1", true},
		{Matched, Cancelled, "resource-1", true}, {Pending, Completed, "", false},
		{Completed, Pending, "", false}, {Cancelled, Matched, "resource-1", false},
		{Pending, Pending, "", true}, {Pending, "unknown", "", false},
	} {
		r := Request{Status: tc.from, ResourceID: tc.assigned}
		err := r.Transition(tc.to)
		if (err == nil) != tc.ok {
			t.Errorf("%s -> %s: %v", tc.from, tc.to, err)
		}
		if tc.ok && r.Status != tc.to {
			t.Error("status not changed")
		}
		if !tc.ok && r.Status != tc.from {
			t.Error("rejected transition mutated state")
		}
	}
}
