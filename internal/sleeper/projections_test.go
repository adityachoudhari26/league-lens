package sleeper

import (
	"encoding/json"
	"math"
	"testing"
)

func TestProjectedPoints(t *testing.T) {
	// Kicker buckets as in a real league: 50+ unscored, 50-59 and 60+ split.
	scoring := map[string]float64{
		"pass_yd": 0.04, "pass_td": 4, "pass_int": -2, "rec": 0.5,
		"fgm_40_49": 4, "fgm_50p": 0, "fgm_50_59": 5, "fgm_60p": 6,
		"fgmiss_40_49": -1, "fgmiss_50p": 0, "fgmiss_50_59": -1,
		"pts_allow_14_20": 1, "yds_allow_200_299": 2,
	}
	cases := []struct {
		name  string
		stats map[string]float64
		want  *float64
	}{
		{"custom scoring, not pts totals", map[string]float64{"pass_yd": 250, "pass_td": 2, "pass_int": 1, "pts_ppr": 99}, ptr(16)},
		{"kicker 50+ scored as 50-59", map[string]float64{"fgm_40_49": 1, "fgm_50p": 0.5, "fgmiss_50p": 0.2}, ptr(6.3)},
		{"kicker split buckets used as is", map[string]float64{"fgm_50p": 1, "fgm_50_59": 0.5, "fgm_60p": 0.5}, ptr(5.5)},
		{"defense buckets", map[string]float64{"pts_allow_14_20": 1, "yds_allow_200_299": 1, "pts_allow": 19}, ptr(3)},
		{"unscored stats are zero", map[string]float64{"rush_yd": 40}, ptr(0)},
		{"negative", map[string]float64{"pass_int": 2, "pass_yd": 10}, ptr(-3.6)},
		{"no stats", map[string]float64{}, nil},
		{"no projection", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectedPoints(tc.stats, scoring)
			if (got == nil) != (tc.want == nil) || got != nil && math.Abs(*got-*tc.want) > 1e-9 {
				t.Fatalf("projectedPoints = %v, want %v", deref(got), deref(tc.want))
			}
		})
	}
}

func TestProjectedPointsLeague50PlusBucket(t *testing.T) {
	scoring := map[string]float64{"fgm_50p": 5, "fgm_50_59": 5}
	if got := projectedPoints(map[string]float64{"fgm_50p": 1}, scoring); *got != 5 {
		t.Fatalf("50+ league projection = %v, want 5 (scored once)", *got)
	}
}

func TestScoringValues(t *testing.T) {
	raw := map[string]json.RawMessage{"rec": json.RawMessage("0.5"), "pass_td": json.RawMessage("null"), "bad": json.RawMessage(`"x"`)}
	if got := scoringValues(raw); len(got) != 1 || got["rec"] != 0.5 {
		t.Fatalf("scoringValues = %v, want rec only", got)
	}
}

func ptr(v float64) *float64 { return &v }

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
