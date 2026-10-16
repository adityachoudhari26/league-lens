package espn

import (
	"testing"
)

func TestNormalizeSideUsesLivePointsWhenPresent(t *testing.T) {
	points := func(value float64) *float64 {
		return &value
	}
	tests := []struct {
		name       string
		points     *float64
		livePoints *float64
		want       float64
	}{
		{name: "live total", points: points(100), livePoints: points(11), want: 11},
		{name: "final total fallback", points: points(133.24), want: 133.24},
		{name: "present live zero", points: points(0), livePoints: points(0), want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			side := normalizeSide(sideResponse{Points: test.points, LivePoints: test.livePoints})
			if side.Score == nil || *side.Score != test.want {
				t.Fatalf("score = %v, want %v", side.Score, test.want)
			}
		})
	}
}
