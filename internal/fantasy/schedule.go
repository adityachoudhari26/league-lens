package fantasy

import "time"

// NFLGame is one team's scheduled game in a scoring week. A team with no game
// in a week whose schedule is loaded is on bye.
type NFLGame struct {
	Season    int
	Week      ScoringPeriod
	Team      string // Abbreviation shared with roster entries, e.g. "KC".
	Opponent  string
	KickoffAt time.Time
}
