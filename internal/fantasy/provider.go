// Package fantasy defines provider-independent contracts for league imports.
// It contains no HTTP, database, credential, or provider response types.
package fantasy

import "context"

// Provider translates a provider's API into fantasy concepts. Credentials,
// HTTP clients, and provider-specific season selection belong to its constructor.
// Implementations must honor cancellation and must not include credentials or
// raw authenticated response bodies in returned errors.
type Provider interface {
	// DescribeLeague resolves a provider league ID to a specific season. This
	// allows the importer to establish its durable sync target before fetching
	// rosters and history. It does not authenticate the application's user.
	DescribeLeague(ctx context.Context, leagueID string) (League, error)

	// FetchLeague returns one coherent import payload for the described league.
	// Required-data failures return an error; they must not become empty data.
	// Optional, unavailable history is omitted, never synthesized. A non-nil
	// error means the importer must not persist the returned payload.
	FetchLeague(ctx context.Context, league League) (LeagueData, error)
}

type ProviderID string

const (
	ProviderSleeper ProviderID = "sleeper"
	ProviderESPN    ProviderID = "espn"
)

// LeagueRef identifies a season, independently of whether a provider reuses
// league IDs across seasons. External IDs are opaque strings, never numbers.
type LeagueRef struct {
	Provider   ProviderID
	ExternalID string
	Season     int
}

// ConnectionSelection belongs to the application's connection, not the fetched
// league. Selecting a public username or a team does not prove account ownership.
// Team IDs are scoped to League. Local user and connection IDs stay in the app.
type ConnectionSelection struct {
	League          LeagueRef
	SelectedTeamIDs []string
}

// DiscoveredLeague is a provider account's league and owned team selection.
type DiscoveredLeague struct {
	Ref      LeagueRef
	Name     string
	TeamID   string
	TeamName string
}
