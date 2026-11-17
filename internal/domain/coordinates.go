package domain

import (
	"fmt"
	"math"
)

// Coordinates identifies a geographic position in degrees.
type Coordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Validate accepts finite geographic coordinates, including zero and bounds.
func (c Coordinates) Validate() error {
	if math.IsNaN(c.Latitude) || math.IsInf(c.Latitude, 0) || c.Latitude < -90 || c.Latitude > 90 || math.IsNaN(c.Longitude) || math.IsInf(c.Longitude, 0) || c.Longitude < -180 || c.Longitude > 180 {
		return fmt.Errorf("%w: latitude must be finite within [-90,90] and longitude within [-180,180]", ErrInvalid)
	}
	return nil
}

// DistanceMeters returns the great-circle distance using the haversine formula
// and the mean Earth radius. Both endpoints must be valid geographic positions.
func (c Coordinates) DistanceMeters(other Coordinates) (float64, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	if err := other.Validate(); err != nil {
		return 0, err
	}
	const meanEarthRadiusMeters = 6371008.8
	const radians = math.Pi / 180
	latitude1, latitude2 := c.Latitude*radians, other.Latitude*radians
	halfLatitude := math.Sin((other.Latitude - c.Latitude) * radians / 2)
	halfLongitude := math.Sin((other.Longitude - c.Longitude) * radians / 2)
	haversine := halfLatitude*halfLatitude + math.Cos(latitude1)*math.Cos(latitude2)*halfLongitude*halfLongitude
	// Roundoff near identical or antipodal points must not leave the domain.
	haversine = math.Max(0, math.Min(1, haversine))
	return 2 * meanEarthRadiusMeters * math.Atan2(math.Sqrt(haversine), math.Sqrt(1-haversine)), nil
}
