package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.LiveScorer = (*Client)(nil)

// FetchLiveMatchup reads the owner's ESPN matchup and its current weekly lineup.
func (c *Client) FetchLiveMatchup(ctx context.Context, league fantasy.League, request fantasy.LiveMatchupRequest) (fantasy.LiveMatchup, error) {
	if league.Ref.Provider != fantasy.ProviderESPN ||
		league.Ref.ExternalID == "" ||
		league.Ref.Season != c.season ||
		request.Week < 1 ||
		request.Period < 1 {
		return fantasy.LiveMatchup{}, fmt.Errorf("valid ESPN league and live matchup request required")
	}
	teamID, err := strconv.Atoi(request.TeamID)
	if err != nil || teamID <= 0 {
		return fantasy.LiveMatchup{}, fmt.Errorf("valid ESPN team ID required")
	}
	filter, err := matchupFilter(teamID, request.Period)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	headers := make(http.Header)
	headers.Set("x-fantasy-filter", filter)
	raw, err := c.readLeagueWithViews(
		ctx,
		league.Ref.ExternalID,
		int(request.Week),
		[]string{"mMatchupScore", "mBoxscore"},
		headers,
	)
	if err != nil {
		return fantasy.LiveMatchup{}, fmt.Errorf("fetch ESPN live matchup: %w", err)
	}

	match, err := findLiveMatchup(raw.Schedule, int(request.Period), teamID)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	weeks := raw.Settings.Schedule.Periods[strconv.Itoa(int(request.Period))]
	firstWeek, lastWeek, err := scoringWeekBounds(weeks)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	if int(request.Week) < firstWeek || int(request.Week) > lastWeek {
		return fantasy.LiveMatchup{}, fmt.Errorf("ESPN matchup period does not include scoring week %d", request.Week)
	}
	normalized, err := normalizeMatchup(*match, weeks, lastWeek, raw.Week)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	if normalized.Status != fantasy.MatchupFinal {
		normalized.Status = fantasy.MatchupInProgress
	}

	key := matchupKey{period: normalized.Period, id: normalized.ExternalID}
	sides := indexMatchupSides([]fantasy.Matchup{normalized})
	players := map[string]fantasy.Player{}
	lineups, err := matchupLineups(key, *match, sides[key], raw.Week, players)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	setLiveScoringCounts(lineups, *match, raw.Week)
	return fantasy.LiveMatchup{
		Matchup: normalized,
		Lineups: lineups,
		Players: sortedPlayers(players),
	}, nil
}

func setLiveScoringCounts(lineups []fantasy.MatchupLineup, match matchupResponse, week int) {
	countsByPlayer := make(map[string]*fantasy.ScoringCounts)
	for _, side := range []*sideResponse{match.Home, match.Away} {
		if side == nil || side.Lineup == nil {
			continue
		}
		for _, entry := range side.Lineup.Entries {
			playerID := strconv.Itoa(entry.Pool.Player.ID)
			countsByPlayer[playerID] = actualScoringCounts(entry.Pool.Player.Stats, week)
		}
	}
	for lineupIndex := range lineups {
		for entryIndex := range lineups[lineupIndex].Entries {
			entry := &lineups[lineupIndex].Entries[entryIndex]
			entry.Scoring = countsByPlayer[entry.PlayerID]
		}
	}
}

func matchupFilter(teamID int, period fantasy.MatchupPeriod) (string, error) {
	var filter struct {
		Schedule struct {
			MatchupPeriodIDs struct {
				Value []int `json:"value"`
			} `json:"filterMatchupPeriodIds"`
			TeamIDs struct {
				Value []int `json:"value"`
			} `json:"filterTeamIds"`
		} `json:"schedule"`
	}
	filter.Schedule.MatchupPeriodIDs.Value = []int{int(period)}
	filter.Schedule.TeamIDs.Value = []int{teamID}
	encoded, err := json.Marshal(filter)
	if err != nil {
		return "", fmt.Errorf("cannot encode ESPN matchup filter: %w", err)
	}
	return string(encoded), nil
}

// findLiveMatchup selects the owner's schedule entry and rejects ambiguous results.
func findLiveMatchup(schedule []matchupResponse, period, teamID int) (*matchupResponse, error) {
	var selected *matchupResponse
	for index := range schedule {
		match := &schedule[index]
		if match.Period != period || !matchIncludesTeam(*match, teamID) {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("multiple ESPN matchups found for team %d in period %d", teamID, period)
		}
		selected = match
	}
	if selected == nil {
		return nil, fmt.Errorf("no ESPN matchup found for team %d in period %d", teamID, period)
	}
	return selected, nil
}

func matchIncludesTeam(match matchupResponse, teamID int) bool {
	return (match.Home != nil && match.Home.TeamID == teamID) ||
		(match.Away != nil && match.Away.TeamID == teamID)
}
