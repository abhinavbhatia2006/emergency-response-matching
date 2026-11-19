package application

import (
	"bytes"
	"encoding/json"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
)

// An omitted coordinate object preserves the district API. An explicit object
// must contain both numbers, and an explicit null is invalid.
func decodeCoordinates(raw json.RawMessage) (*domain.Coordinates, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var input struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Latitude == nil || input.Longitude == nil {
		return nil, domain.ErrInvalid
	}
	return &domain.Coordinates{Latitude: *input.Latitude, Longitude: *input.Longitude}, nil
}

func (s *Service) selectResource(request domain.Request, resources []domain.Resource) (domain.Resource, *float64, error) {
	if request.Coordinates == nil {
		resource, err := domain.SelectResource(request, resources)
		return resource, nil, err
	}
	if request.Coordinates.Validate() != nil {
		return domain.Resource{}, nil, ErrUnavailable
	}
	var selected domain.Resource
	var shortest float64
	for _, resource := range resources {
		if resource.ID == "" || resource.Type != request.Type || !resource.Availability || resource.Status != domain.Available || resource.Coordinates == nil {
			continue
		}
		distance, err := request.Coordinates.DistanceMeters(*resource.Coordinates)
		if err != nil {
			return domain.Resource{}, nil, ErrUnavailable
		}
		if distance <= s.maxDistanceMeters && (selected.ID == "" || distance < shortest || (distance == shortest && resource.ID < selected.ID)) {
			selected, shortest = resource, distance
		}
	}
	if selected.ID == "" {
		return domain.Resource{}, nil, domain.ErrNoMatch
	}
	return selected, &shortest, nil
}
