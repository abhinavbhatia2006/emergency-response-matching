// Package domain defines emergency assistance data and business rules.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid = errors.New("invalid input or transition")
	ErrNoMatch = errors.New("no suitable resource available")
)

type Type string

const (
	Food           Type = "food"
	Water          Type = "water"
	Medicine       Type = "medicine"
	Transportation Type = "transportation"
	Shelter        Type = "shelter"
)

type RequestStatus string

const (
	Pending   RequestStatus = "pending"
	Matched   RequestStatus = "matched"
	Completed RequestStatus = "completed"
	Cancelled RequestStatus = "cancelled"
)

type ResourceStatus string

const (
	Available   ResourceStatus = "available"
	Reserved    ResourceStatus = "reserved"
	Unavailable ResourceStatus = "unavailable"
)

type Request struct {
	ID         string        `json:"id"`
	Type       Type          `json:"type"`
	Location   string        `json:"location"`
	Details    string        `json:"details"`
	Status     RequestStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	ResourceID string        `json:"resource_id,omitempty"`
}
type Resource struct {
	ID           string         `json:"id"`
	Type         Type           `json:"type"`
	Location     string         `json:"location"`
	Availability bool           `json:"availability"`
	Status       ResourceStatus `json:"status"`
}

func normalize(kind, location string) (Type, string, error) {
	if !utf8.ValidString(location) {
		return "", "", fmt.Errorf("%w: location must be valid UTF-8", ErrInvalid)
	}
	t := Type(strings.ToLower(strings.TrimSpace(kind)))
	switch t {
	case Food, Water, Medicine, Transportation, Shelter:
	default:
		return "", "", fmt.Errorf("%w: unsupported type", ErrInvalid)
	}
	location = strings.ToLower(strings.TrimSpace(location))
	if !utf8.ValidString(location) || location == "" || utf8.RuneCountInString(location) > 200 {
		return "", "", fmt.Errorf("%w: location must contain 1-200 characters", ErrInvalid)
	}
	return t, location, nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func NewRequest(kind, location, details string, now time.Time) (Request, error) {
	t, location, err := normalize(kind, location)
	if err != nil {
		return Request{}, err
	}
	details = strings.TrimSpace(details)
	if !utf8.ValidString(details) || details == "" || utf8.RuneCountInString(details) > 4000 || now.IsZero() {
		return Request{}, fmt.Errorf("%w: details must contain 1-4000 characters and creation time is required", ErrInvalid)
	}
	id, err := newID()
	if err != nil {
		return Request{}, err
	}
	return Request{ID: id, Type: t, Location: location, Details: details, Status: Pending, CreatedAt: now.UTC()}, nil
}

func NewResource(kind, location string, available bool) (Resource, error) {
	t, location, err := normalize(kind, location)
	if err != nil {
		return Resource{}, err
	}
	id, err := newID()
	if err != nil {
		return Resource{}, err
	}
	status := Unavailable
	if available {
		status = Available
	}
	return Resource{ID: id, Type: t, Location: location, Availability: available, Status: status}, nil
}

// Transition validates lifecycle changes. The caller persists the change and
// coordinates resource reservation/release in the same logical transaction.
func (r *Request) Transition(next RequestStatus) error {
	valid := false
	switch r.Status {
	case Pending:
		valid = next == Pending || next == Cancelled || (next == Matched && r.ResourceID != "")
	case Matched:
		valid = r.ResourceID != "" && (next == Matched || next == Completed || next == Cancelled)
	case Completed:
		valid = next == Completed
	case Cancelled:
		valid = next == Cancelled
	}
	if !valid {
		return fmt.Errorf("%w: cannot change %s to %s", ErrInvalid, r.Status, next)
	}
	r.Status = next
	return nil
}

// SelectResource returns a candidate without reserving it or changing inputs.
// Stored entities must have been validated and normalized at creation.
func SelectResource(request Request, resources []Resource) (Resource, error) {
	if request.Status != Pending {
		return Resource{}, fmt.Errorf("%w: matching requires a pending request", ErrInvalid)
	}
	var selected Resource
	for _, r := range resources {
		if r.ID != "" && r.Type == request.Type && r.Location == request.Location && r.Availability && r.Status == Available && (selected.ID == "" || r.ID < selected.ID) {
			selected = r
		}
	}
	if selected.ID == "" {
		return Resource{}, ErrNoMatch
	}
	return selected, nil
}
