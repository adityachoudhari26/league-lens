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

// MatchupLineup is one team's lineup in a matchup. It identifies the matchup
// by Period and ExternalID, and TeamID must be one of that matchup's sides.
// Every player must appear in LeagueData.Players. A missing lineup means
// unavailable, not empty.
type MatchupLineup struct {
	Period    MatchupPeriod
	MatchupID string // Matchup.ExternalID.
	TeamID    string
	Entries   []LineupEntry
}

type LineupEntry struct {
	PlayerID       string
	ProviderSlotID string // Retains unfamiliar provider slots without guessing.
	Slot           RosterSlot
	Starter        bool // Provider-reported; bench and IR slots are not starters.

	// Points is the player's actual league-scored total for the matchup, as
	// with MatchupSide.Score. Nil means not reported, zero is a real score, and
	// projections must never be substituted.
	Points *float64
}
