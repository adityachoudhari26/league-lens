package fantasy

// MatchupPeriod is a provider's positive matchup round, not a scoring week.
// It is scoped to the league season.
type MatchupPeriod int

type MatchupStatus string

const (
	MatchupScheduled  MatchupStatus = "scheduled"
	MatchupInProgress MatchupStatus = "in_progress"
	MatchupFinal      MatchupStatus = "final"
	MatchupUnknown    MatchupStatus = "unknown"
)

type Matchup struct {
	ExternalID     string // Unique within the league's matchup period.
	Period         MatchupPeriod
	ScoringPeriods []ScoringPeriod // Known constituent weeks; nil when unavailable.
	TeamA          MatchupSide
	TeamB          *MatchupSide // nil means no assigned opponent, not automatically a bye.
	IsBye          bool         // true only for a confirmed bye; TeamB must be nil.
	Status         MatchupStatus
}

type MatchupSide struct {
	TeamID string

	// Score is the actual total for the whole matchup period, with provider
	// adjustments already applied. Nil means unknown/unplayed, zero is a real
	// score, and projections must never be substituted for actual points.
	Score *float64
}
