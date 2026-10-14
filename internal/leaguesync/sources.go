package leaguesync

import (
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

// SourceFactory builds the provider client used to refresh a target.
type SourceFactory interface {
	SourceFor(Target) (fantasy.Provider, error)
}

// ProviderSources builds live ESPN and Sleeper clients. ESPN cookies come from
// the server environment until encrypted connection credentials exist.
type ProviderSources struct {
	ESPN espn.Credentials
}

// setupError is a stable, credential-free failure code raised before an import starts.
type setupError string

func (e setupError) Error() string { return string(e) }

func (p ProviderSources) SourceFor(target Target) (fantasy.Provider, error) {
	switch target.Selection.League.Provider {
	case fantasy.ProviderSleeper:
		return sleeper.NewClient(nil), nil
	case fantasy.ProviderESPN:
		return p.espnSource(target)
	default:
		return nil, setupError("unsupported_provider")
	}
}

// espnSource refuses cookies for a different ESPN account than the one that
// imported the league, so one account's view never overwrites another's.
func (p ProviderSources) espnSource(target Target) (fantasy.Provider, error) {
	if p.ESPN.S2 == "" || p.ESPN.SWID == "" {
		return nil, setupError("espn_credentials_missing")
	}

	client := espn.NewClient(nil, p.ESPN, target.Selection.League.Season)
	if client.AccountID() != target.ExternalAccountID {
		return nil, setupError("espn_account_mismatch")
	}
	return client, nil
}
