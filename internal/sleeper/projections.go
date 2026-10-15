package sleeper

import (
	"bytes"
	"encoding/json"
)

// scoringValues reads the league's scoring_settings as points per stat key,
// skipping null and non-numeric entries.
func scoringValues(raw map[string]json.RawMessage) map[string]float64 {
	scoring := map[string]float64{}
	for key, value := range raw {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var points float64
		if json.Unmarshal(value, &points) == nil {
			scoring[key] = points
		}
	}
	return scoring
}

// projectionsByPlayer indexes one week's projected stat lines by player ID.
func projectionsByPlayer(rows []Projection) map[string]map[string]float64 {
	byPlayer := make(map[string]map[string]float64, len(rows))
	for _, row := range rows {
		byPlayer[row.PlayerID] = row.Stats
	}
	return byPlayer
}

// projectedPoints scores a projected stat line with the league's scoring, the
// way Sleeper's app does, since the line's generic pts_* totals ignore custom
// scoring. Nil when the player has no projected stats.
func projectedPoints(stats, scoring map[string]float64) *float64 {
	if len(stats) == 0 {
		return nil
	}
	var points float64
	for key, value := range stats {
		points += value * scoring[key]
	}
	points += longFieldGoalPoints(stats, scoring, "fgm")
	points += longFieldGoalPoints(stats, scoring, "fgmiss")
	return &points
}

// longFieldGoalPoints scores projected 50+ yard kicks for leagues that score
// the 50-59 and 60+ buckets instead of 50+. Projections only have the 50+
// bucket, so its kicks are scored as 50-59, where nearly all of them land.
func longFieldGoalPoints(stats, scoring map[string]float64, prefix string) float64 {
	_, split59 := stats[prefix+"_50_59"]
	_, split60 := stats[prefix+"_60p"]
	if split59 || split60 || scoring[prefix+"_50p"] != 0 {
		return 0
	}
	return stats[prefix+"_50p"] * scoring[prefix+"_50_59"]
}
