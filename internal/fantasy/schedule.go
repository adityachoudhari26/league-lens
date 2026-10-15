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

// NFLGameState is a game's progress on the live scoreboard.
type NFLGameState string

const (
	NFLGameScheduled NFLGameState = "scheduled"
	NFLGameLive      NFLGameState = "live" // includes halftime
	NFLGameFinal     NFLGameState = "final"
	NFLGamePostponed NFLGameState = "postponed" // ended without completing: postponed, canceled, suspended
)

// NFLScoreboard is one moment's view of a week's NFL games.
type NFLScoreboard struct {
	Season     int
	SeasonType int // ESPN's IDs: 1 preseason, 2 regular season, 3 postseason.
	Week       ScoringPeriod
	Games      []NFLGameStatus
}

// NFLGameStatus describes one game's participants and current scoreboard state.
type NFLGameStatus struct {
	ID        string // ESPN event ID.
	HomeTeam  string // Abbreviations shared with roster entries, e.g. "KC".
	AwayTeam  string
	KickoffAt time.Time
	State     NFLGameState
	Period    int           // 0 before kickoff, 1–4 quarters, 5+ overtime.
	Clock     time.Duration // Time left in the current period.
}
