package fantasy

import "time"

// ScoringPeriod is a positive NFL scoring week. It must not be confused with
// MatchupPeriod, which may span more than one scoring week.
type ScoringPeriod int

type SnapshotOrigin string

const (
	SnapshotObserved SnapshotOrigin = "observed"
	SnapshotHistory  SnapshotOrigin = "provider_history"
)

type RosterCoverage string

const (
	CoverageFull    RosterCoverage = "full"
	CoverageLineup  RosterCoverage = "lineup"
	CoveragePartial RosterCoverage = "partial"
)

type RosterSnapshot struct {
	TeamID        string
	ScoringPeriod ScoringPeriod
	Origin        SnapshotOrigin
	Coverage      RosterCoverage
	Entries       []RosterEntry

	// ObservedAt is when this data was fetched, including historical data.
	// EffectiveAt is set only when the provider supplies the actual effective
	// time; never invent a timestamp from a week number.
	ObservedAt  time.Time
	EffectiveAt *time.Time

	// Record is scoped to this snapshot's period. Never copy today's standings
	// onto a historical roster. Nil means the period's record is unavailable.
	Record *TeamRecord
}

// A full snapshot with zero entries confirms an empty roster. To represent an
// unavailable roster, omit the snapshot instead. A lineup-only or partial
// snapshot must never be treated as proof that the bench is empty.
type RosterEntry struct {
	PlayerID          string
	ProviderSlotID    string // Retains unfamiliar provider slots without guessing.
	Slot              RosterSlot
	EligiblePositions []Position // nil means unavailable, not ineligible everywhere.

	// Availability as reported by the provider when the snapshot was fetched.
	// Empty values mean the provider did not supply them.
	InjuryStatus InjuryStatus `json:",omitempty"`
	ProTeam      string       `json:",omitempty"` // NFL team abbreviation, e.g. "KC".
}

// InjuryStatus is a provider injury designation reduced to the cases that
// change a start/sit decision. Unfamiliar designations become InjuryUnknown.
type InjuryStatus string

const (
	InjuryActive       InjuryStatus = "active"
	InjuryQuestionable InjuryStatus = "questionable"
	InjuryDoubtful     InjuryStatus = "doubtful"
	InjuryOut          InjuryStatus = "out"
	InjuryReserve      InjuryStatus = "ir"
	InjuryUnknown      InjuryStatus = "unknown"
)

type RosterSlot string

const (
	SlotQB        RosterSlot = "QB"
	SlotRB        RosterSlot = "RB"
	SlotWR        RosterSlot = "WR"
	SlotTE        RosterSlot = "TE"
	SlotK         RosterSlot = "K"
	SlotDefense   RosterSlot = "DEF"
	SlotFlex      RosterSlot = "FLEX"
	SlotSuperflex RosterSlot = "SUPERFLEX"
	SlotBench     RosterSlot = "BENCH"
	SlotIR        RosterSlot = "IR"
	SlotTaxi      RosterSlot = "TAXI"
	SlotUnknown   RosterSlot = "UNKNOWN"
)
