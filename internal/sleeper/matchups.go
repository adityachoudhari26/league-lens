package sleeper

import (
	"fmt"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"sort"
	"strconv"
)

func normalizeMatchups(rows []Matchup, week int, historical bool) ([]fantasy.Matchup, error) {
	groups := map[string][]Matchup{}
	for _, row := range rows {
		key := "bye-" + strconv.Itoa(row.RosterID)
		if row.MatchupID != nil && *row.MatchupID > 0 {
			key = strconv.Itoa(*row.MatchupID)
		}
		groups[key] = append(groups[key], row)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]fantasy.Matchup, 0, len(groups))
	for _, key := range keys {
		opponents := groups[key]
		if len(opponents) > 2 {
			return nil, fmt.Errorf("more than two teams in matchup")
		}
		sort.Slice(opponents, func(i, j int) bool { return opponents[i].RosterID < opponents[j].RosterID })
		status := fantasy.MatchupUnknown
		if historical {
			status = fantasy.MatchupFinal
		}
		matchup := fantasy.Matchup{ExternalID: key, Period: fantasy.MatchupPeriod(week), ScoringPeriods: []fantasy.ScoringPeriod{fantasy.ScoringPeriod(week)}, TeamA: normalizeSide(opponents[0]), Status: status}
		if len(opponents) == 2 {
			opponent := normalizeSide(opponents[1])
			matchup.TeamB = &opponent
		}
		// A missing matchup ID alone doesn't prove a bye; retain unknown opponents.
		result = append(result, matchup)
	}
	return result, nil
}
func normalizeSide(raw Matchup) fantasy.MatchupSide {
	points := raw.Points
	if raw.CustomPoints != nil {
		points = raw.CustomPoints
	}
	return fantasy.MatchupSide{TeamID: strconv.Itoa(raw.RosterID), Score: points}
}
