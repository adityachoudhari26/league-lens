package main

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

func prepareSource(ctx context.Context, options importOptions) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
	if options.provider == "espn" {
		return prepareESPN(ctx, options)
	}
	client := sleeper.NewClient(nil)
	client.PlayersCachePath, _ = sleeper.DefaultPlayersCachePath()
	league, err := client.DescribeLeague(ctx, options.leagueID)
	if err != nil {
		return nil, "", fantasy.ConnectionSelection{}, err
	}
	accountID, selection, err := client.ResolveSelection(ctx, options.username, league)
	return client, accountID, selection, err
}
func prepareESPN(ctx context.Context, options importOptions) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
	credentials := espn.Credentials{S2: os.Getenv("ESPN_S2"), SWID: os.Getenv("ESPN_SWID")}
	if credentials.S2 == "" || credentials.SWID == "" {
		return nil, "", fantasy.ConnectionSelection{}, errors.New("export ESPN_S2 and ESPN_SWID before importing a private ESPN league")
	}
	client := espn.NewClient(nil, credentials, options.season)
	league, err := client.DescribeLeague(ctx, options.leagueID)
	if err != nil {
		return nil, "", fantasy.ConnectionSelection{}, err
	}
	selection := fantasy.ConnectionSelection{League: league.Ref, SelectedTeamIDs: []string{strconv.Itoa(options.teamID)}}
	// The shared importer validates the selected team against fetched teams.
	return client, client.AccountID(), selection, nil
}
