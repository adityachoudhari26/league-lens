// Package fantasy defines provider-independent contracts for league imports.
// It contains no HTTP, database, credential, or provider response types.
package fantasy

import (
	"context"
	"slices"
	"time"
)

// FreeAgent is a provider's available player with optional point and ownership data.
type FreeAgent struct {
	ExternalPlayerID string   `json:"external_player_id"`
	Name             string   `json:"name"`
	Position         string   `json:"position"`
	NFLTeam          string   `json:"nfl_team"`
	InjuryStatus     string   `json:"injury_status"`
	Status           string   `json:"status"`
	ProjectedPoints  *float64 `json:"projected_points,omitempty"`
	SeasonPoints     *float64 `json:"season_points,omitempty"`
	PercentRostered  *float64 `json:"percent_rostered,omitempty"`
}

// FreeAgentQuery selects the scoring week, position, and result count.
type FreeAgentQuery struct {
	Week     int
	Position string
	Limit    int
}

// FreeAgentLister returns provider-specific available players for a league.
type FreeAgentLister interface {
	ListFreeAgents(ctx context.Context, league League, query FreeAgentQuery) ([]FreeAgent, error)
}

// TransactionType identifies the provider-independent kind of a league move.
type TransactionType string

const (
	// TransactionAdd is a free-agent acquisition.
	TransactionAdd TransactionType = "add"
	// TransactionDrop is a free-agent release without an acquisition.
	TransactionDrop TransactionType = "drop"
	// TransactionWaiver is a completed waiver claim.
	TransactionWaiver TransactionType = "waiver"
	// TransactionTrade is an executed trade.
	TransactionTrade TransactionType = "trade"
)

// Transaction is one completed league transaction.
type Transaction struct {
	ExternalID string
	Type       TransactionType
	ExecutedAt time.Time
	Moves      []TransactionMove
	FAABBid    *int // Only set for waiver claims in leagues using FAAB.
}

// TransactionMove describes one player's movement between provider teams.
// An empty FromTeamID means free agency or waivers; an empty ToTeamID means dropped.
type TransactionMove struct {
	ExternalPlayerID string
	FromTeamID       string
	ToTeamID         string
	DefenseTeam      string
}

// TransactionLister returns completed league transactions for scoring weeks.
type TransactionLister interface {
	ListTransactions(ctx context.Context, league League, weeks []int) ([]Transaction, error)
}

// Provider translates a provider's API into fantasy concepts. Credentials,
// HTTP clients, and provider-specific season selection belong to its constructor.
// Implementations must honor cancellation and must not include credentials or
// raw authenticated response bodies in returned errors.
type Provider interface {
	// DescribeLeague resolves a provider league ID to a specific season. This
	// allows the importer to establish its durable sync target before fetching
	// rosters and history. It does not authenticate the application's user.
	DescribeLeague(ctx context.Context, leagueID string) (League, error)

	// FetchLeague returns one coherent import payload for the described league,
	// narrowed by request. Required-data failures return an error; they must not
	// become empty data. Optional, unavailable history is omitted, never
	// synthesized. A non-nil error means the importer must not persist the
	// returned payload.
	FetchLeague(ctx context.Context, league League, request FetchRequest) (LeagueData, error)
}

// FetchRequest narrows a FetchLeague read. The zero value reads full history.
type FetchRequest struct {
	// SkipWeeks are past scoring weeks the app already holds as final. The
	// provider must not make a per-week read for them, and LeagueData leaves out
	// their roster history snapshots and lineups (omitted means keep saved).
	// Matchups the provider gets without a per-week read (ESPN's season
	// schedule in the current-week response) may still be included; a provider
	// that reads matchups per week (Sleeper) leaves those weeks' matchups and
	// projections out too, and does not list them in CompleteMatchupPeriods.
	// The current scoring week is never skipped, even if listed.
	SkipWeeks []ScoringPeriod
}

// SkipsWeek reports whether a provider should leave week unread: it is listed
// in SkipWeeks and is not the current scoring week, which is always read.
func (r FetchRequest) SkipsWeek(week, current ScoringPeriod) bool {
	return week != current && slices.Contains(r.SkipWeeks, week)
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
