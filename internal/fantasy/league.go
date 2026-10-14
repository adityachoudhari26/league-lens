package fantasy

import (
	"encoding/json"
	"time"
)

type League struct {
	Ref       LeagueRef
	Name      string
	TeamCount int
	Settings  LeagueSettings
}

// LeagueData is fetched before the database write transaction. Teams contains
// the complete league, including teams without an owner. Every team referenced
// by a roster or matchup must appear here, and every roster player must appear
// in Players. Player records include only encountered players.
//
// Missing snapshots and matchups mean unknown/unavailable, not deletion.
// Empty Rosters means no snapshots were available, not that teams are empty.
type LeagueData struct {
	League    League
	Teams     []Team
	Players   []Player
	Rosters   []RosterSnapshot
	Matchups  []Matchup
	Lineups   []MatchupLineup
	FetchedAt time.Time

	// Only these periods have an authoritative, complete matchup list. Within
	// them the importer may remove obsolete pairings after corrections. An
	// omitted period must retain previously stored matchups. A listed period
	// with no matchups explicitly confirms an empty schedule for that period.
	CompleteMatchupPeriods []MatchupPeriod
}

type Team struct {
	ExternalID string
	Name       string
	Record     *TeamRecord // nil means unavailable; a zero record is valid.
	Rank       *int        // nil means unknown; known ranks are one-based.
}

type TeamRecord struct {
	Wins          int
	Losses        int
	Ties          int
	PointsFor     *float64
	PointsAgainst *float64
}

type LeagueSettings struct {
	RosterSlots []SlotDefinition

	// These are the common, flat-rate scoring rules the adapter can express
	// faithfully. An absent stat is unknown, not necessarily worth zero points.
	ScoringRules []ScoringRule

	// Supplemental scoring bonuses and other settings that aren't normalized
	// yet. Must be a JSON object, contain no credentials/member data, and not
	// be required by shared import logic. Nil means unavailable.
	ProviderSettings json.RawMessage
}

type SlotDefinition struct {
	Slot              RosterSlot
	Count             int
	EligiblePositions []Position
}

// Stat is a canonical name such as passing_yards or receptions, never a
// provider's numeric stat ID. Adapters must use the same names for the same stat.
type Stat string

const (
	StatPassingYards        Stat = "passing_yards"
	StatPassingTouchdowns   Stat = "passing_touchdowns"
	StatInterceptions       Stat = "interceptions_thrown"
	StatRushingYards        Stat = "rushing_yards"
	StatRushingTouchdowns   Stat = "rushing_touchdowns"
	StatReceivingYards      Stat = "receiving_yards"
	StatReceivingTouchdowns Stat = "receiving_touchdowns"
	StatReceptions          Stat = "receptions"
	StatFumblesLost         Stat = "fumbles_lost"
)

// ScoringRule awards Points for each PerUnits of Stat (which must be positive).
// For example, 1 point per 25 passing yards. Bonuses, ranges, and rounding rules
// that cannot be represented exactly stay in ProviderSettings until normalized.
type ScoringRule struct {
	Stat     Stat
	Points   float64
	PerUnits float64
}
