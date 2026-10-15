package connections

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

// ImportSpec describes one provider league and team selection to import.
type ImportSpec struct {
	Provider, LeagueID string
	Season, TeamID     int
	Username           string
	ESPN               espn.Credentials
}

// PrepareSource builds a provider and validated owner selection for one import.
func PrepareSource(ctx context.Context, spec ImportSpec) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
	switch fantasy.ProviderID(spec.Provider) {
	case fantasy.ProviderESPN:
		return prepareESPN(ctx, spec)
	case fantasy.ProviderSleeper:
		return prepareSleeper(ctx, spec)
	default:
		return nil, "", fantasy.ConnectionSelection{}, fmt.Errorf("unsupported provider")
	}
}

func prepareESPN(ctx context.Context, spec ImportSpec) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
	if spec.ESPN.S2 == "" || spec.ESPN.SWID == "" {
		return nil, "", fantasy.ConnectionSelection{}, errors.New("export ESPN_S2 and ESPN_SWID before importing a private ESPN league")
	}
	if spec.Season < 1900 || spec.Season > 2200 || spec.TeamID <= 0 || spec.LeagueID == "" {
		return nil, "", fantasy.ConnectionSelection{}, errors.New("valid ESPN league, season, and team selection required")
	}
	client := espn.NewClient(nil, spec.ESPN, spec.Season)
	league, err := client.DescribeLeague(ctx, spec.LeagueID)
	if err != nil {
		return nil, "", fantasy.ConnectionSelection{}, err
	}
	selection := fantasy.ConnectionSelection{
		League:          league.Ref,
		SelectedTeamIDs: []string{strconv.Itoa(spec.TeamID)},
	}
	return client, client.AccountID(), selection, nil
}

func prepareSleeper(ctx context.Context, spec ImportSpec) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
	if spec.Username == "" || spec.LeagueID == "" {
		return nil, "", fantasy.ConnectionSelection{}, errors.New("sleeper username and league ID required")
	}
	client := sleeper.NewClient(nil)
	league, err := client.DescribeLeague(ctx, spec.LeagueID)
	if err != nil {
		return nil, "", fantasy.ConnectionSelection{}, err
	}
	if spec.Season > 0 && league.Ref.Season != spec.Season {
		return nil, "", fantasy.ConnectionSelection{}, errors.New("sleeper league season changed")
	}
	accountID, selection, err := client.ResolveSelection(ctx, spec.Username, league)
	if err != nil {
		return nil, "", fantasy.ConnectionSelection{}, err
	}
	if spec.TeamID > 0 {
		teamID := strconv.Itoa(spec.TeamID)
		if !contains(selection.SelectedTeamIDs, teamID) {
			return nil, "", fantasy.ConnectionSelection{}, errors.New("selected team is not owned by Sleeper user")
		}
		selection.SelectedTeamIDs = []string{teamID}
	}
	client.AvailabilityTeamIDs = selection.SelectedTeamIDs
	return client, accountID, selection, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
