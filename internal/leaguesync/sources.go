package leaguesync

import (
	"context"
	"encoding/json"

	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

// SourceFactory builds the provider client used to refresh a target.
type SourceFactory interface {
	SourceFor(Target) (fantasy.Provider, error)
	NFLSchedule(ctx context.Context, season int) ([]fantasy.NFLGame, error)
	NFLScoreboard(ctx context.Context) (fantasy.NFLScoreboard, error)
	NFLWeekScoreboard(ctx context.Context, season, week int) (fantasy.NFLScoreboard, error)
	NFLGamePlays(ctx context.Context, eventID string) ([]fantasy.NFLPlay, error)
	NewsSource() news.Source
}

// ProviderSources builds live ESPN and Sleeper clients, preferring saved
// encrypted ESPN cookies and falling back to the server environment.
type ProviderSources struct {
	ESPN              espn.Credentials
	Box               *secrets.Box
	PlayerCatalogPath string
}

type encryptedESPNCredentials struct {
	S2   string `json:"s2"`
	SWID string `json:"swid"`
}

// setupError is a stable, credential-free failure code raised before an import starts.
type setupError string

func (e setupError) Error() string { return string(e) }

func (p ProviderSources) SourceFor(target Target) (fantasy.Provider, error) {
	switch target.Selection.League.Provider {
	case fantasy.ProviderSleeper:
		client := sleeper.NewClient(nil)
		client.AddAvailabilityTeams(target.Selection.SelectedTeamIDs...)
		client.AddAvailabilityTeams(target.AvailabilityTeamIDs...)
		client.CatalogPath = p.PlayerCatalogPath
		return client, nil
	case fantasy.ProviderESPN:
		return p.espnSource(target)
	default:
		return nil, setupError("unsupported_provider")
	}
}

// NFLSchedule reads ESPN's public schedule, which serves leagues from every provider.
func (p ProviderSources) NFLSchedule(ctx context.Context, season int) ([]fantasy.NFLGame, error) {
	return espn.FetchNFLSchedule(ctx, nil, season)
}

// NFLGameContext returns public betting lines and forecasts for one NFL week.
func (ProviderSources) NFLGameContext(ctx context.Context, week int) (fantasy.NFLWeekGameContext, error) {
	return espn.FetchNFLGameContext(ctx, nil, week)
}

// NFLScoreboard reads the public ESPN scoreboard shared by every provider.
func (ProviderSources) NFLScoreboard(ctx context.Context) (fantasy.NFLScoreboard, error) {
	return espn.FetchNFLScoreboard(ctx, nil)
}

// NFLWeekScoreboard reads the public ESPN scoreboard for a regular-season week.
func (ProviderSources) NFLWeekScoreboard(ctx context.Context, season, week int) (fantasy.NFLScoreboard, error) {
	return espn.FetchNFLWeekScoreboard(ctx, nil, season, week)
}

// NFLGamePlays reads public ESPN play-by-play without provider credentials.
func (ProviderSources) NFLGamePlays(ctx context.Context, eventID string) ([]fantasy.NFLPlay, error) {
	return espn.FetchNFLGamePlays(ctx, nil, eventID)
}

// NewsSource returns ESPN's public fantasy player-news feed adapter.
func (ProviderSources) NewsSource() news.Source { return espn.NewsSource{} }

// espnSource refuses cookies for a different ESPN account than the one that
// imported the league, so one account's view never overwrites another's.
func (p ProviderSources) espnSource(target Target) (fantasy.Provider, error) {
	credentials := p.ESPN
	if len(target.EncryptedCredentials) > 0 {
		if p.Box == nil {
			return nil, setupError("espn_credentials_unavailable")
		}
		plaintext, err := p.Box.Open(target.EncryptedCredentials, target.ConnectionID.Bytes[:])
		if err != nil {
			return nil, setupError("espn_credentials_unavailable")
		}
		var stored encryptedESPNCredentials
		if json.Unmarshal(plaintext, &stored) != nil || stored.S2 == "" || stored.SWID == "" {
			return nil, setupError("espn_credentials_unavailable")
		}
		credentials = espn.Credentials{S2: stored.S2, SWID: stored.SWID}
	}
	if credentials.S2 == "" || credentials.SWID == "" {
		return nil, setupError("espn_credentials_missing")
	}

	client := espn.NewClient(nil, credentials, target.Selection.League.Season)
	if client.AccountID() != target.ExternalAccountID {
		return nil, setupError("espn_account_mismatch")
	}
	return client, nil
}
