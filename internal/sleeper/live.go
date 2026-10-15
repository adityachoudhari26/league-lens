package sleeper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.LiveScorer = (*Client)(nil)

type liveStatsRow struct {
	PlayerID string             `json:"player_id"`
	Stats    map[string]float64 `json:"stats"`
}

// FetchLiveMatchup reads one Sleeper matchup and optionally labels scoring plays.
func (c *Client) FetchLiveMatchup(ctx context.Context, league fantasy.League, request fantasy.LiveMatchupRequest) (fantasy.LiveMatchup, error) {
	if league.Ref.Provider != fantasy.ProviderSleeper ||
		league.Ref.ExternalID == "" ||
		league.Ref.Season < 1900 ||
		league.Ref.Season > 2200 ||
		request.Week < 1 ||
		request.Period < 1 {
		return fantasy.LiveMatchup{}, fmt.Errorf("valid Sleeper league and live matchup request required")
	}
	rosterID, err := strconv.Atoi(request.TeamID)
	if err != nil || rosterID < 1 {
		return fantasy.LiveMatchup{}, fmt.Errorf("valid Sleeper roster ID required")
	}

	path := "/league/" + url.PathEscape(league.Ref.ExternalID) + "/matchups/" + strconv.Itoa(int(request.Week))
	var rows []Matchup
	if err := c.get(ctx, path, &rows); err != nil {
		return fantasy.LiveMatchup{}, err
	}
	selectedRows, err := liveMatchupRows(rows, rosterID)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	matchups, err := normalizeMatchups(selectedRows, int(request.Week), false)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	if len(matchups) != 1 {
		return fantasy.LiveMatchup{}, fmt.Errorf("expected one Sleeper matchup for roster %d", rosterID)
	}
	matchup := matchups[0]
	matchup.Period = request.Period
	matchup.ScoringPeriods = []fantasy.ScoringPeriod{request.Week}
	positions, err := sleeperRosterPositions(league)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	lineups, err := normalizeLineups(selectedRows, []fantasy.Matchup{matchup}, positions, nil, nil, nil)
	if err != nil {
		return fantasy.LiveMatchup{}, err
	}
	if request.IncludeScoringCounts {
		stats, ok := c.fetchLiveScoringStats(ctx, league.Ref.Season, request.Week)
		if ok {
			applyLiveScoringCounts(lineups, stats)
		}
	}
	return fantasy.LiveMatchup{
		Matchup: matchup,
		Lineups: lineups,
		Players: encounteredPlayers(nil, lineups),
	}, nil
}

// liveMatchupRows selects the owner's matchup row and its opponent, if present.
func liveMatchupRows(rows []Matchup, rosterID int) ([]Matchup, error) {
	var owner *Matchup
	for index := range rows {
		if rows[index].RosterID != rosterID {
			continue
		}
		if owner != nil {
			return nil, fmt.Errorf("multiple Sleeper matchup rows for roster %d", rosterID)
		}
		owner = &rows[index]
	}
	if owner == nil {
		return nil, fmt.Errorf("no Sleeper matchup found for roster %d", rosterID)
	}
	selected := []Matchup{*owner}
	if owner.MatchupID == nil || *owner.MatchupID <= 0 {
		return selected, nil
	}
	for index := range rows {
		if rows[index].RosterID != rosterID &&
			rows[index].MatchupID != nil &&
			*rows[index].MatchupID == *owner.MatchupID {
			selected = append(selected, rows[index])
		}
	}
	return selected, nil
}

// sleeperRosterPositions returns the provider's ordered lineup slots for normalization.
func sleeperRosterPositions(league fantasy.League) ([]string, error) {
	var settings struct {
		Positions []string `json:"roster_positions"`
	}
	if err := json.Unmarshal(league.Settings.ProviderSettings, &settings); err != nil {
		return nil, fmt.Errorf("cannot read Sleeper roster positions: %w", err)
	}
	if len(settings.Positions) == 0 {
		return nil, fmt.Errorf("Sleeper roster positions are unavailable")
	}
	return settings.Positions, nil
}

// fetchLiveScoringStats returns false when the optional weekly stats request fails.
func (c *Client) fetchLiveScoringStats(ctx context.Context, season int, week fantasy.ScoringPeriod) (map[string]map[string]float64, bool) {
	path := "/" + strconv.Itoa(season) + "/" + strconv.Itoa(int(week))
	var rows []liveStatsRow
	if err := c.getURL(ctx, c.StatsURL+path+"?season_type=regular", "/stats/nfl"+path, &rows); err != nil {
		return nil, false
	}
	stats := make(map[string]map[string]float64, len(rows))
	for _, row := range rows {
		if row.PlayerID != "" {
			stats[row.PlayerID] = row.Stats
		}
	}
	return stats, true
}

// applyLiveScoringCounts labels stats for every lineup player and zero-fills missing starters.
func applyLiveScoringCounts(lineups []fantasy.MatchupLineup, stats map[string]map[string]float64) {
	for lineupIndex := range lineups {
		for entryIndex := range lineups[lineupIndex].Entries {
			entry := &lineups[lineupIndex].Entries[entryIndex]
			playerStats, found := stats[entry.PlayerID]
			if !found && !entry.Starter {
				continue
			}
			counts := fantasy.ScoringCounts{}
			if found {
				counts.Touchdowns = int(playerStats["pass_td"] +
					playerStats["rush_td"] +
					playerStats["rec_td"] +
					playerStats["def_td"])
				counts.FieldGoals = int(playerStats["fgm"])
			}
			entry.Scoring = &counts
		}
	}
}
