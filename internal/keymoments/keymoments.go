package keymoments

import "math"

// PointChange is one starter's league-scored points and scoring counts before and after a live tick.
// Nil counts are unknown.
type PointChange struct {
	PointsBefore, PointsAfter         float64
	TouchdownsBefore, TouchdownsAfter *int
	FieldGoalsBefore, FieldGoalsAfter *int
}

// Kind identifies the type of scoring moment.
type Kind string

const (
	// KindTouchdown identifies a newly recorded touchdown.
	KindTouchdown Kind = "touchdown"
	// KindFieldGoal identifies a newly recorded field goal.
	KindFieldGoal Kind = "field_goal"
	// KindPlay identifies a point change without a known scoring-count increase.
	KindPlay Kind = "play"
)

// MinPlayPoints is the smallest absolute point change listed as a play.
const MinPlayPoints = 1.0

// Moment describes a classified scoring moment; Points is after-minus-before, so negative values mean a drop.
type Moment struct {
	Kind   Kind
	Points float64
}

// Classify labels provider league-scored points; scoring counts only determine the moment kind.
func Classify(change PointChange) (Moment, bool) {
	points := change.PointsAfter - change.PointsBefore
	if countsIncreased(change.TouchdownsBefore, change.TouchdownsAfter) {
		return Moment{Kind: KindTouchdown, Points: points}, true
	}
	if countsIncreased(change.FieldGoalsBefore, change.FieldGoalsAfter) {
		return Moment{Kind: KindFieldGoal, Points: points}, true
	}
	if math.Abs(points)+1e-9 < MinPlayPoints {
		return Moment{}, false
	}
	return Moment{Kind: KindPlay, Points: points}, true
}

func countsIncreased(before, after *int) bool {
	return before != nil && after != nil && *after > *before
}
