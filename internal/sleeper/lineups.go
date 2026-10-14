package sleeper

import (
	"fmt"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

// normalizeLineups builds each side's lineup for one week's normalized
// matchups from the same /matchups rows. Starters are positional against the
// league's roster_positions and "0" marks an empty starter slot. Matchup rows
// list reserve and taxi players without saying which they are, so designations
// (the current rosters) label them only for the observed week; past weeks show
// every non-starter as bench, like roster history. Rows without players or
// starters are unavailable and get no lineup.
func normalizeLineups(rows []Matchup, matchups []fantasy.Matchup, positions []string, designations map[int]Roster) ([]fantasy.MatchupLineup, error) {
	rowsByTeam := map[string]Matchup{}
	for _, row := range rows {
		rowsByTeam[strconv.Itoa(row.RosterID)] = row
	}
	var lineups []fantasy.MatchupLineup
	for _, matchup := range matchups {
		for _, teamID := range matchupTeamIDs(matchup) {
			row := rowsByTeam[teamID]
			if row.Players == nil && row.Starters == nil {
				continue
			}
			entries, err := lineupEntries(row, positions, designations[row.RosterID])
			if err != nil {
				return nil, fmt.Errorf("week %d lineup for roster %s: %w", matchup.Period, teamID, err)
			}
			lineups = append(lineups, fantasy.MatchupLineup{Period: matchup.Period, MatchupID: matchup.ExternalID, TeamID: teamID, Entries: entries})
		}
	}
	return lineups, nil
}

func matchupTeamIDs(matchup fantasy.Matchup) []string {
	if matchup.TeamB == nil {
		return []string{matchup.TeamA.TeamID}
	}
	return []string{matchup.TeamA.TeamID, matchup.TeamB.TeamID}
}

// lineupEntries reuses the roster slot rules and attaches players_points.
// Starter points add up to Sleeper's points, not a custom_points override.
// rosterEntries gives only starters a slot index, which marks them as starters.
func lineupEntries(row Matchup, positions []string, roster Roster) ([]fantasy.LineupEntry, error) {
	reserve := benchedOnly(roster.Reserve, row)
	taxi := benchedOnly(roster.Taxi, row)
	entries, _, err := rosterEntries(row.Players, row.Starters, reserve, taxi, positions)
	if err != nil {
		return nil, err
	}
	lineup := make([]fantasy.LineupEntry, 0, len(entries))
	for _, entry := range entries {
		slot, _ := normalizeSlot(entry.Label)
		lineup = append(lineup, fantasy.LineupEntry{PlayerID: entry.ID, ProviderSlotID: entry.Slot, Slot: slot, Starter: entry.Slot != "", Points: playerPoints(row.PlayersPoints, entry.ID)})
	}
	return lineup, nil
}

// benchedOnly keeps the designated players that the matchup row has as
// non-starters, so a roster change between the two requests can't fail the import.
func benchedOnly(designated []string, row Matchup) []string {
	inRow := map[string]bool{}
	for _, id := range row.Players {
		inRow[id] = true
	}
	for _, id := range row.Starters {
		delete(inRow, id)
	}
	var kept []string
	for _, id := range designated {
		if inRow[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

func playerPoints(points map[string]float64, playerID string) *float64 {
	value, reported := points[playerID]
	if !reported {
		return nil
	}
	return &value
}
