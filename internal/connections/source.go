package connections

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// ImportSpec describes one provider league and team selection to import.
type ImportSpec struct {
	Provider, LeagueID string
	Season, TeamID     int
	Username           string
	ESPN               espn.Credentials
}

// PreparedSource is a provider ready to import one league: the owner's
// account, the validated selection, and the league as just described.
type PreparedSource struct {
	Provider  fantasy.Provider
	AccountID string
	Selection fantasy.ConnectionSelection
	League    fantasy.League
}

// PrepareSource builds a provider and validated owner selection for one import.
func PrepareSource(ctx context.Context, spec ImportSpec) (PreparedSource, error) {
	switch fantasy.ProviderID(spec.Provider) {
	case fantasy.ProviderESPN:
		return prepareESPN(ctx, spec)
	case fantasy.ProviderSleeper:
		return prepareSleeper(ctx, spec)
	default:
		return PreparedSource{}, fmt.Errorf("unsupported provider")
	}
}

func prepareESPN(ctx context.Context, spec ImportSpec) (PreparedSource, error) {
	if spec.ESPN.S2 == "" || spec.ESPN.SWID == "" {
		return PreparedSource{}, errors.New("export ESPN_S2 and ESPN_SWID before importing a private ESPN league")
	}
	if spec.Season < 1900 || spec.Season > 2200 || spec.TeamID <= 0 || spec.LeagueID == "" {
		return PreparedSource{}, errors.New("valid ESPN league, season, and team selection required")
	}
	client := espn.NewClient(nil, spec.ESPN, spec.Season)
	league, err := client.DescribeLeague(ctx, spec.LeagueID)
	if err != nil {
		return PreparedSource{}, err
	}
	selection := fantasy.ConnectionSelection{
		League:          league.Ref,
		SelectedTeamIDs: []string{strconv.Itoa(spec.TeamID)},
	}
	return PreparedSource{Provider: client, AccountID: client.AccountID(), Selection: selection, League: league}, nil
}

func prepareSleeper(ctx context.Context, spec ImportSpec) (PreparedSource, error) {
	if spec.Username == "" || spec.LeagueID == "" {
		return PreparedSource{}, errors.New("sleeper username and league ID required")
	}
	client := sleeper.NewClient(nil)
	league, err := client.DescribeLeague(ctx, spec.LeagueID)
	if err != nil {
		return PreparedSource{}, err
	}
	if spec.Season > 0 && league.Ref.Season != spec.Season {
		return PreparedSource{}, errors.New("sleeper league season changed")
	}
	accountID, selection, err := client.ResolveSelection(ctx, spec.Username, league)
	if err != nil {
		return PreparedSource{}, err
	}
	if spec.TeamID > 0 {
		teamID := strconv.Itoa(spec.TeamID)
		if !contains(selection.SelectedTeamIDs, teamID) {
			return PreparedSource{}, errors.New("selected team is not owned by Sleeper user")
		}
		selection.SelectedTeamIDs = []string{teamID}
	}
	client.AvailabilityTeamIDs = selection.SelectedTeamIDs
	return PreparedSource{Provider: client, AccountID: accountID, Selection: selection, League: league}, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// AddLeagueAvailability makes a Sleeper import also look up availability for
// every other app user's team in the league, because the league's roster rows
// are shared. Other providers already return every team's availability.
func AddLeagueAvailability(ctx context.Context, queries *store.Queries, prepared PreparedSource) error {
	client, ok := prepared.Provider.(*sleeper.Client)
	if !ok {
		return nil
	}
	teams, err := queries.ListLeagueAvailabilityTeams(ctx, store.ListLeagueAvailabilityTeamsParams{
		Provider:         string(prepared.League.Ref.Provider),
		ExternalLeagueID: prepared.League.Ref.ExternalID,
		Season:           int32(prepared.League.Ref.Season),
	})
	if err != nil {
		return err
	}
	client.AddAvailabilityTeams(teams...)
	return nil
}
