package fantasy

import "context"

// NFLTeamCount is the number of NFL franchises.
const NFLTeamCount = 32

// DefenseVsPositionRater reports how many fantasy points each NFL defense allows per game, by position.
type DefenseVsPositionRater interface {
	DefenseVsPosition(ctx context.Context, league League) (DefenseVsPositionTable, error)
}

// DefenseVsPositionTable contains current-season points allowed by NFL team and position.
// For DST, a team is the opponent offense: the points D/ST units score against it.
type DefenseVsPositionTable struct {
	Scoring   string                     // "league", "espn", "ppr", "half_ppr", or "standard".
	Positions map[string]PositionDefense // Keys are QB, RB, WR, TE, K, and DST.
}

// PositionDefense reports the league average and each defense's per-game points allowed.
type PositionDefense struct {
	LeagueAverage float64                  // The provider's per-game position average.
	ByTeam        map[string]DefenseRating // Keys are stored NFL team abbreviations.
}

// DefenseRating is a defense's per-game points allowed and one-based difficulty rank.
type DefenseRating struct {
	PointsAllowedPerGame float64 // Fantasy points allowed to the position per game.
	Rank                 int     // 1 is fewest allowed; 32 is most allowed.
}
