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
		r, err := NewRequest(" "+strings.ToUpper(kind)+" ", " North District ", " Drinking water needed ", now)
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
		{"fuel", "north", "needed", time.Now()}, {"water", " ", "needed", time.Now()},
		{"water", "north", " ", time.Now()}, {"water", "north", "needed", time.Time{}},
		{"water", strings.Repeat("x", 201), "needed", time.Now()}, {"water", "north", strings.Repeat("x", 4001), time.Now()},
	} {
		if _, err := NewRequest(tc.kind, tc.location, tc.details, tc.now); !errors.Is(err, ErrInvalid) {
			t.Errorf("expected invalid input, got %v", err)
		}
	}
}

func TestResourceAvailability(t *testing.T) {
	for _, available := range []bool{true, false} {
		r, err := NewResource(" WATER ", " North ", available)
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
	for _, input := range [][2]string{{"fuel", "north"}, {"water", " "}} {
		if _, err := NewResource(input[0], input[1], true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected invalid input, got %v", err)
		}
	}
}

func TestRejectInvalidUTF8Location(t *testing.T) {
	for _, location := range []string{"\xff", "\xfe"} {
		if _, err := NewRequest("water", location, "needed", time.Now()); !errors.Is(err, ErrInvalid) {
			t.Errorf("request accepted malformed location: %v", err)
		}
		if _, err := NewResource("water", location, true); !errors.Is(err, ErrInvalid) {
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

func TestSelectResource(t *testing.T) {
	request := Request{Type: Water, Location: "north", Status: Pending}
	resources := []Resource{
		{ID: "wrong-type", Type: Food, Location: "north", Availability: true, Status: Available},
		{ID: "wrong-location", Type: Water, Location: "south", Availability: true, Status: Available},
		{ID: "reserved", Type: Water, Location: "north", Availability: true, Status: Reserved},
		{ID: "unavailable", Type: Water, Location: "north", Availability: false, Status: Available},
		{ID: "z", Type: Water, Location: "north", Availability: true, Status: Available},
		{ID: "a", Type: Water, Location: "north", Availability: true, Status: Available},
	}
	got, err := SelectResource(request, resources)
	if err != nil || got.ID != "a" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if resources[0].ID != "wrong-type" || resources[4].ID != "z" {
		t.Fatal("mutated input order")
	}
	if _, err := SelectResource(request, resources[:4]); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("expected no match, got %v", err)
	}
	if _, err := SelectResource(request, nil); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("expected no match, got %v", err)
	}
	request.Status = Completed
	if _, err := SelectResource(request, resources); !errors.Is(err, ErrInvalid) {
		t.Fatalf("must reject terminal request: %v", err)
	}
}
