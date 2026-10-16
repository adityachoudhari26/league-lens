package fantasy

import "time"

// NFLWeekGameContext contains betting lines and forecasts for one NFL week.
type NFLWeekGameContext struct {
	Season int
	Week   ScoringPeriod
	Games  []NFLGameContext
}

// NFLGameContext describes the betting lines and forecast for one NFL game.
type NFLGameContext struct {
	HomeTeam, AwayTeam string
	KickoffAt          time.Time
	State              NFLGameState
	Odds               *NFLGameOdds
	Weather            *NFLGameWeather
}

// NFLGameOdds contains the provider's display line and total.
type NFLGameOdds struct {
	Line      string
	OverUnder *float64
	Source    string
}

// NFLGameWeather contains the game-time forecast when ESPN provides one.
type NFLGameWeather struct {
	Condition    string
	TemperatureF *int
}
