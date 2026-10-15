package winchance

import (
	"math"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

// Starter is one lineup starter's projected points and fraction of their game remaining.
type Starter struct {
	Projected *float64
	Remaining float64
}

// Side is a matchup side's current score and remaining starters.
type Side struct {
	Score    float64
	Starters []Starter
}

// Result contains the estimated probability for side A and each side's projected final score.
type Result struct {
	WinChanceA, ProjectedA, ProjectedB float64
}

// SpreadPerProjectedPoint is the fitted standard deviation per projected point.
// The fit was 0.555–0.565 over 2026 weeks 1–6, using 1,529 starters.
const SpreadPerProjectedPoint = 0.55

// Estimate projects each side as its current score plus each starter's
// projection times remaining game fraction. It assumes independent starter
// variance of (SpreadPerProjectedPoint × projection)² × remaining fraction and
// returns the normal CDF of the projected score difference.
func Estimate(a, b Side) Result {
	projectedA, varianceA := projectSide(a)
	projectedB, varianceB := projectSide(b)
	totalVariance := varianceA + varianceB
	if totalVariance == 0 {
		switch {
		case projectedA > projectedB:
			return Result{WinChanceA: 1, ProjectedA: projectedA, ProjectedB: projectedB}
		case projectedA < projectedB:
			return Result{ProjectedA: projectedA, ProjectedB: projectedB}
		default:
			return Result{WinChanceA: 0.5, ProjectedA: projectedA, ProjectedB: projectedB}
		}
	}

	z := (projectedA - projectedB) / math.Sqrt(totalVariance)
	return Result{
		WinChanceA: 0.5 * (1 + math.Erf(z/math.Sqrt2)),
		ProjectedA: projectedA,
		ProjectedB: projectedB,
	}
}

func projectSide(side Side) (float64, float64) {
	projected := side.Score
	variance := 0.0
	for _, starter := range side.Starters {
		if starter.Projected == nil {
			continue
		}
		points := math.Max(0, *starter.Projected)
		remaining := math.Max(0, math.Min(1, starter.Remaining))
		projected += points * remaining
		spread := SpreadPerProjectedPoint * points
		variance += spread * spread * remaining
	}
	return projected, variance
}

// GameRemaining returns the fraction of regulation time left. Scheduled games
// and live games before period 1 have full time remaining; live overtime, final,
// and postponed games have none.
func GameRemaining(game fantasy.NFLGameStatus) float64 {
	switch game.State {
	case fantasy.NFLGameScheduled:
		return 1
	case fantasy.NFLGameFinal, fantasy.NFLGamePostponed:
		return 0
	case fantasy.NFLGameLive:
		if game.Period < 1 {
			return 1
		}
		if game.Period >= 5 {
			return 0
		}
		fraction := float64(time.Duration(4-game.Period)*15*time.Minute+game.Clock) / float64(60*time.Minute)
		return math.Max(0, math.Min(1, fraction))
	default:
		return 0
	}
}
