// Package config reads shared application runtime settings.
package config

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
)

// MaxMatchDistanceMeters supplies the same geographic radius to API and worker.
func MaxMatchDistanceMeters() (float64, error) {
	value := strings.TrimSpace(os.Getenv("MAX_MATCH_DISTANCE_METERS"))
	if value == "" {
		return 50000, nil
	}
	distance, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(distance) || math.IsInf(distance, 0) || distance <= 0 || distance > 20015000 {
		return 0, errors.New("MAX_MATCH_DISTANCE_METERS must be finite, greater than zero and at most 20015000")
	}
	return distance, nil
}
