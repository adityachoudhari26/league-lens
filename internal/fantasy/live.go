package fantasy

import "context"

// ScoringCounts are a player's scoring plays so far in a scoring week. They only
// label point changes (touchdown vs field goal); points always come from the
// provider's league-scored totals.
type ScoringCounts struct {
	Touchdowns int // every kind: passing, rushing, receiving, defensive/return
	FieldGoals int // made field goals
}

// LiveMatchupRequest names the owner's matchup for a single-week matchup period.
type LiveMatchupRequest struct {
	Week   ScoringPeriod
	Period MatchupPeriod
	TeamID string // the owner's provider team ID (Sleeper roster ID)
}

// LiveMatchup is the current state of one matchup. It follows the LeagueData
// contracts: every lineup player appears in Players.
type LiveMatchup struct {
	Matchup Matchup
	Lineups []MatchupLineup // both sides when the matchup has an opponent
	Players []Player
}

// LiveScorer is an optional provider capability for reading one matchup's current scoring.
// Full-sync LeagueData never sets LineupEntry.Scoring. FetchLiveMatchup sets
// Scoring for every entry when counts are available; otherwise it is nil.
// Counts are never inferred from points.
type LiveScorer interface {
	FetchLiveMatchup(ctx context.Context, league League, request LiveMatchupRequest) (LiveMatchup, error)
}
