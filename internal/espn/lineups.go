package espn

import (
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

const (
	espnBenchSlotID = 20
	espnIRSlotID    = 21

	espnActualStatSource    = 0
	espnProjectedStatSource = 1
	espnSingleWeekStatSplit = 1
)

type matchupKey struct {
	period fantasy.MatchupPeriod
	id     string
}

// appendLineups adds the lineups ESPN's mBoxscore view reports for raw.Week.
// A weekly response only carries rosterForCurrentScoringPeriod for that week's
// matchups, so each history response contributes its own week.
func appendLineups(data *fantasy.LeagueData, players map[string]fantasy.Player, raw leagueResponse) error {
	matchupSides := indexMatchupSides(data.Matchups)
	for _, match := range singleWeekMatchups(raw) {
		key := lineupMatchupKey(match)
		lineups, err := matchupLineups(key, match, matchupSides[key], raw.Week, players)
		if err != nil {
			return err
		}
		data.Lineups = append(data.Lineups, lineups...)
	}
	return nil
}

// indexMatchupSides maps each normalized matchup to its team IDs, so lineups
// attach only to existing matchup sides.
func indexMatchupSides(matchups []fantasy.Matchup) map[matchupKey]map[string]bool {
	sides := make(map[matchupKey]map[string]bool, len(matchups))
	for _, matchup := range matchups {
		key := matchupKey{period: matchup.Period, id: matchup.ExternalID}
		sides[key] = map[string]bool{matchup.TeamA.TeamID: true}
		if matchup.TeamB != nil {
			sides[key][matchup.TeamB.TeamID] = true
		}
	}
	return sides
}

// singleWeekMatchups returns the schedule entries whose matchup period is
// exactly raw.Week. Multi-week matchup periods are skipped: one week's lineup
// is not the matchup's lineup.
func singleWeekMatchups(raw leagueResponse) []matchupResponse {
	var matchups []matchupResponse
	for _, match := range raw.Schedule {
		weeks := raw.Settings.Schedule.Periods[strconv.Itoa(match.Period)]
		if len(weeks) == 1 && weeks[0] == raw.Week {
			matchups = append(matchups, match)
		}
	}
	return matchups
}

func lineupMatchupKey(match matchupResponse) matchupKey {
	return matchupKey{period: fantasy.MatchupPeriod(match.Period), id: strconv.Itoa(match.ID)}
}

// matchupLineups normalizes the lineups ESPN reported for one matchup's sides.
// Sides that are not sides of the normalized matchup, or that have no lineup
// block, get no lineup.
func matchupLineups(
	key matchupKey,
	match matchupResponse,
	normalizedTeams map[string]bool,
	week int,
	players map[string]fantasy.Player,
) ([]fantasy.MatchupLineup, error) {
	var lineups []fantasy.MatchupLineup
	for _, side := range []*sideResponse{match.Home, match.Away} {
		if !hasReportedLineup(side, normalizedTeams) {
			continue
		}
		lineup, err := normalizeLineup(key, *side, week, players)
		if err != nil {
			return nil, err
		}
		lineups = append(lineups, lineup)
	}
	return lineups, nil
}

func hasReportedLineup(side *sideResponse, normalizedTeams map[string]bool) bool {
	return side != nil && side.Lineup != nil && normalizedTeams[strconv.Itoa(side.TeamID)]
}

// normalizeLineup converts one side's weekly lineup and records its players,
// since players dropped after a past week may appear only in that lineup.
func normalizeLineup(key matchupKey, side sideResponse, week int, players map[string]fantasy.Player) (fantasy.MatchupLineup, error) {
	lineup := fantasy.MatchupLineup{Period: key.period, MatchupID: key.id, TeamID: strconv.Itoa(side.TeamID)}
	for _, entry := range side.Lineup.Entries {
		player, err := normalizePlayer(entry)
		if err != nil {
			return lineup, err
		}
		addMissingPlayer(players, player)
		lineup.Entries = append(lineup.Entries, normalizeLineupEntry(player.ExternalID, entry, week))
	}
	return lineup, nil
}

// addMissingPlayer records a lineup-only player. Existing player metadata
// wins, as with rosters.
func addMissingPlayer(players map[string]fantasy.Player, player fantasy.Player) {
	if _, exists := players[player.ExternalID]; !exists {
		players[player.ExternalID] = player
	}
}

func normalizeLineupEntry(playerID string, entry entryResponse, week int) fantasy.LineupEntry {
	slot, _ := normalizeSlot(entry.Slot)
	return fantasy.LineupEntry{
		PlayerID:        playerID,
		ProviderSlotID:  strconv.Itoa(entry.Slot),
		Slot:            slot,
		Starter:         isStarterSlot(entry.Slot),
		Points:          actualPoints(entry, week),
		ProjectedPoints: projectedPoints(entry.Pool.Player.Stats, week),
	}
}

// isStarterSlot reports whether ESPN counts a slot toward the team score:
// every slot except bench and IR.
func isStarterSlot(slot int) bool {
	return slot != espnBenchSlotID && slot != espnIRSlotID
}

// actualPoints returns the player's league-scored total for week. ESPN reports
// appliedStatTotal as 0 for unplayed weeks and bye weeks, so the value counts
// only when the player has an actual stat line for week.
func actualPoints(entry entryResponse, week int) *float64 {
	if _, found := weekStatLine(entry.Pool.Player.Stats, espnActualStatSource, week); !found {
		return nil
	}
	return entry.Pool.AppliedStatTotal
}

// actualScoringCounts returns touchdown and field-goal counts from the week's actual stat line.
func actualScoringCounts(stats []statResponse, week int) *fantasy.ScoringCounts {
	actual, found := weekStatLine(stats, espnActualStatSource, week)
	if !found {
		return nil
	}
	// Stat IDs 63, 93, and 101 are six-point TD stats but were absent from observed lines.
	touchdowns := int(actual.Stats["4"] +
		actual.Stats["25"] +
		actual.Stats["43"] +
		actual.Stats["102"] +
		actual.Stats["103"] +
		actual.Stats["104"])
	return &fantasy.ScoringCounts{
		Touchdowns: touchdowns,
		FieldGoals: int(actual.Stats["83"]),
	}
}

// projectedPoints returns ESPN's league-scored projection for week, the
// appliedTotal of its projected stat line. ESPN keeps that line after the game,
// so finished weeks keep their projection. Nil when ESPN has no projected
// line for week.
func projectedPoints(stats []statResponse, week int) *float64 {
	projection, found := weekStatLine(stats, espnProjectedStatSource, week)
	if !found {
		return nil
	}
	return projection.AppliedTotal
}

// weekStatLine finds the single-week stat line from source for week.
func weekStatLine(stats []statResponse, source, week int) (statResponse, bool) {
	for _, stat := range stats {
		if stat.SourceID == source && stat.SplitTypeID == espnSingleWeekStatSplit && stat.ScoringPeriodID == week {
			return stat, true
		}
	}
	return statResponse{}, false
}
