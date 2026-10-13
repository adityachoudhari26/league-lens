package espn

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func appendMatchups(data *fantasy.LeagueData, raw leagueResponse) error {
	for _, match := range raw.Schedule {
		weeks := raw.Settings.Schedule.Periods[strconv.Itoa(match.Period)]
		first, last, err := scoringWeekBounds(weeks)
		if err != nil {
			return err
		}
		if first > raw.Week {
			continue
		} // Future schedules are a separate checkpoint.
		normalized, err := normalizeMatchup(match, weeks, last, raw.Week)
		if err != nil {
			return err
		}
		data.Matchups = append(data.Matchups, normalized)
	}
	data.CompleteMatchupPeriods = completeMatchupPeriods(data.Matchups, len(raw.Teams))
	return nil
}
func scoringWeekBounds(weeks []int) (int, int, error) {
	if len(weeks) == 0 {
		return 0, 0, fmt.Errorf("ESPN matchup scoring weeks unavailable")
	}
	first, last := weeks[0], weeks[0]
	for _, week := range weeks {
		if week < 1 || week > 18 {
			return 0, 0, fmt.Errorf("invalid ESPN matchup scoring week")
		}
		if week < first {
			first = week
		}
		if week > last {
			last = week
		}
	}
	return first, last, nil
}
func normalizeMatchup(raw matchupResponse, weeks []int, last, current int) (fantasy.Matchup, error) {
	matchup := fantasy.Matchup{ExternalID: strconv.Itoa(raw.ID), Period: fantasy.MatchupPeriod(raw.Period), Status: fantasy.MatchupUnknown}
	if raw.ID <= 0 || raw.Period <= 0 || (raw.Home == nil && raw.Away == nil) {
		return matchup, fmt.Errorf("invalid ESPN matchup")
	}
	for _, week := range weeks {
		matchup.ScoringPeriods = append(matchup.ScoringPeriods, fantasy.ScoringPeriod(week))
	}
	if last <= current && (raw.Winner == "HOME" || raw.Winner == "AWAY" || raw.Winner == "TIE") {
		matchup.Status = fantasy.MatchupFinal
	}
	if raw.Home != nil {
		matchup.TeamA = normalizeSide(*raw.Home)
		if raw.Away != nil {
			side := normalizeSide(*raw.Away)
			matchup.TeamB = &side
		}
	} else {
		matchup.TeamA = normalizeSide(*raw.Away)
	}
	// Missing opponent alone does not prove a bye.
	return matchup, nil
}
func completeMatchupPeriods(matchups []fantasy.Matchup, teamCount int) []fantasy.MatchupPeriod {
	teamsByPeriod := map[fantasy.MatchupPeriod]map[string]bool{}
	for _, match := range matchups {
		if teamsByPeriod[match.Period] == nil {
			teamsByPeriod[match.Period] = map[string]bool{}
		}
		teamsByPeriod[match.Period][match.TeamA.TeamID] = true
		if match.TeamB != nil {
			teamsByPeriod[match.Period][match.TeamB.TeamID] = true
		}
	}
	var periods []fantasy.MatchupPeriod
	for period, teams := range teamsByPeriod {
		if len(teams) == teamCount {
			periods = append(periods, period)
		}
	}
	sort.Slice(periods, func(i, j int) bool { return periods[i] < periods[j] })
	return periods
}
func normalizeSide(raw sideResponse) fantasy.MatchupSide {
	return fantasy.MatchupSide{TeamID: strconv.Itoa(raw.TeamID), Score: raw.Points}
}
