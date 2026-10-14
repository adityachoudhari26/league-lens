package importer

import (
	"fmt"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"math"
	"strings"
)

func validateData(data fantasy.LeagueData, described fantasy.League, selection fantasy.ConnectionSelection) error {
	if data.League.Ref != described.Ref || selection.League != described.Ref {
		return fmt.Errorf("provider returned a different league")
	}
	if len(selection.SelectedTeamIDs) > 1 {
		return fmt.Errorf("more than one selected team in this league")
	}
	if data.FetchedAt.IsZero() || data.League.TeamCount < 1 || len(data.Teams) != data.League.TeamCount {
		return fmt.Errorf("incomplete league data")
	}
	teams := map[string]bool{}
	for _, team := range data.Teams {
		if team.ExternalID == "" || teams[team.ExternalID] || strings.TrimSpace(team.Name) == "" {
			return fmt.Errorf("invalid or duplicate team")
		}
		teams[team.ExternalID] = true
		if team.Rank != nil && (*team.Rank < 1 || *team.Rank > math.MaxInt32) {
			return fmt.Errorf("invalid rank")
		}
		if err := validateRecord(team.Record); err != nil {
			return err
		}
	}
	for _, id := range selection.SelectedTeamIDs {
		if !teams[id] {
			return fmt.Errorf("selected team is not in league")
		}
	}
	players, err := validatePlayers(data.Players, data.League.Ref.Provider)
	if err != nil {
		return err
	}
	snapshots := map[string]bool{}
	for _, roster := range data.Rosters {
		if err = validateRoster(roster, teams, players); err != nil {
			return err
		}
		key := fmt.Sprintf("%s:%d:%s:%s", roster.TeamID, roster.ScoringPeriod, roster.Origin, roster.Coverage)
		if snapshots[key] {
			return fmt.Errorf("duplicate roster snapshot")
		}
		snapshots[key] = true
	}
	periods := map[fantasy.MatchupPeriod]bool{}
	for _, period := range data.CompleteMatchupPeriods {
		if period < 1 || period > math.MaxInt32 || periods[period] {
			return fmt.Errorf("invalid complete matchup period")
		}
		periods[period] = true
	}
	matchups := map[string]bool{}
	for _, matchup := range data.Matchups {
		if err = validateMatchup(matchup, teams); err != nil {
			return err
		}
		key := fmt.Sprintf("%d:%s", matchup.Period, matchup.ExternalID)
		if matchups[key] {
			return fmt.Errorf("duplicate matchup")
		}
		matchups[key] = true
	}
	return nil
}
func validatePlayers(players []fantasy.Player, provider fantasy.ProviderID) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, player := range players {
		if player.ExternalID == "" || ids[player.ExternalID] || strings.TrimSpace(player.Name) == "" {
			return nil, fmt.Errorf("invalid or duplicate player")
		}
		if player.Kind != fantasy.PlayerPerson && player.Kind != fantasy.PlayerTeamDefense {
			return nil, fmt.Errorf("invalid player kind")
		}
		ids[player.ExternalID] = true
		aliases := map[fantasy.PlayerNamespace]string{fantasy.PlayerNamespace(provider): player.ExternalID}
		for _, alias := range player.KnownIDs {
			switch alias.Namespace {
			case fantasy.PlayerIDSleeper, fantasy.PlayerIDESPN, fantasy.PlayerIDGSIS, fantasy.PlayerIDMFL:
			default:
				return nil, fmt.Errorf("unsupported player namespace")
			}
			if alias.ExternalID == "" {
				return nil, fmt.Errorf("empty player alias")
			}
			if prior, found := aliases[alias.Namespace]; found && prior != alias.ExternalID {
				return nil, fmt.Errorf("conflicting player alias")
			}
			aliases[alias.Namespace] = alias.ExternalID
		}
	}
	return ids, nil
}
func validateRoster(roster fantasy.RosterSnapshot, teams, players map[string]bool) error {
	if !teams[roster.TeamID] || roster.ScoringPeriod < 1 || roster.ScoringPeriod > math.MaxInt32 || roster.ObservedAt.IsZero() {
		return fmt.Errorf("invalid roster identity or period")
	}
	switch roster.Origin {
	case fantasy.SnapshotObserved, fantasy.SnapshotHistory:
	default:
		return fmt.Errorf("invalid snapshot origin")
	}
	switch roster.Coverage {
	case fantasy.CoverageFull, fantasy.CoverageLineup, fantasy.CoveragePartial:
	default:
		return fmt.Errorf("invalid roster coverage")
	}
	seen := map[string]bool{}
	for _, entry := range roster.Entries {
		if !players[entry.PlayerID] || seen[entry.PlayerID] || entry.Slot == "" {
			return fmt.Errorf("invalid roster entry")
		}
		seen[entry.PlayerID] = true
	}
	return validateRecord(roster.Record)
}
func validateMatchup(matchup fantasy.Matchup, teams map[string]bool) error {
	if matchup.ExternalID == "" || matchup.Period < 1 || matchup.Period > math.MaxInt32 || !teams[matchup.TeamA.TeamID] {
		return fmt.Errorf("invalid matchup")
	}
	switch matchup.Status {
	case fantasy.MatchupScheduled, fantasy.MatchupInProgress, fantasy.MatchupFinal, fantasy.MatchupUnknown:
	default:
		return fmt.Errorf("invalid matchup status")
	}
	if !finiteScore(matchup.TeamA.Score) {
		return fmt.Errorf("invalid matchup score")
	}
	if matchup.TeamB != nil {
		if matchup.IsBye || !teams[matchup.TeamB.TeamID] || matchup.TeamA.TeamID == matchup.TeamB.TeamID || !finiteScore(matchup.TeamB.Score) {
			return fmt.Errorf("invalid matchup opponent")
		}
	}
	seen := map[fantasy.ScoringPeriod]bool{}
	for _, week := range matchup.ScoringPeriods {
		if week < 1 || week > math.MaxInt32 || seen[week] {
			return fmt.Errorf("invalid matchup scoring period")
		}
		seen[week] = true
	}
	return nil
}
func validateRecord(record *fantasy.TeamRecord) error {
	if record != nil && (record.Wins < 0 || record.Losses < 0 || record.Ties < 0 || !finiteScore(record.PointsFor) || !finiteScore(record.PointsAgainst)) {
		return fmt.Errorf("invalid team record")
	}
	return nil
}
func finiteScore(score *float64) bool {
	return score == nil || (!math.IsNaN(*score) && !math.IsInf(*score, 0))
}
