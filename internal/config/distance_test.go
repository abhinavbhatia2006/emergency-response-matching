package config

import "testing"

func TestMaxMatchDistanceMeters(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    float64
		invalid bool
	}{
		{"", 50000, false}, {"25000", 25000, false}, {"0.5", 0.5, false}, {" 50000 ", 50000, false},
		{"0", 0, true}, {"-1", 0, true}, {"NaN", 0, true}, {"Inf", 0, true}, {"bad", 0, true}, {"20015001", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("MAX_MATCH_DISTANCE_METERS", tc.value)
			got, err := MaxMatchDistanceMeters()
			if (err != nil) != tc.invalid {
				t.Fatalf("value %q: error=%v", tc.value, err)
			}
			if err == nil && got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
