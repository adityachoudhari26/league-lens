package sleeper

import "fmt"

func importPeriod(state State, leagueSeason string) (int, error) {
	if state.Season != leagueSeason {
		return 0, fmt.Errorf("only the current NFL season is supported")
	}
	var period int
	switch state.SeasonType {
	case "regular":
		period = state.Week
	case "pre":
		period = 1
	case "post":
		period = 18
	default:
		return 0, fmt.Errorf("unsupported NFL season state")
	}
	if period < 1 || period > 18 {
		return period, fmt.Errorf("invalid NFL week")
	}
	return period, nil
}

func validateLeagueRosters(rosters []Roster, header Header) (map[int]bool, error) {
	if len(rosters) != header.League.TotalRosters {
		return nil, fmt.Errorf("incomplete league rosters")
	}
	rosterIDs := map[int]bool{}
	ownsTeam := false
	for _, roster := range rosters {
		if roster.ID <= 0 || rosterIDs[roster.ID] {
			return nil, fmt.Errorf("invalid or duplicate roster ID")
		}
		rosterIDs[roster.ID] = true
		if roster.OwnerID == header.User.ID {
			ownsTeam = true
		}
		for _, coOwner := range roster.CoOwners {
			if coOwner == header.User.ID {
				ownsTeam = true
			}
		}
	}
	if !ownsTeam {
		return nil, fmt.Errorf("Sleeper user does not own a team in this league")
	}
	return rosterIDs, nil
}

func validateWeeklyRosters(matchups []Matchup, rosterIDs map[int]bool, week int) error {
	seen := map[int]bool{}
	for _, matchup := range matchups {
		if !rosterIDs[matchup.RosterID] || seen[matchup.RosterID] {
			return fmt.Errorf("invalid weekly roster")
		}
		seen[matchup.RosterID] = true
	}
	// Empty weeks are unavailable history, never empty roster snapshots.
	if len(matchups) > 0 && len(matchups) != len(rosterIDs) {
		return fmt.Errorf("incomplete matchup week %d", week)
	}
	return nil
}
