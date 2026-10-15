package replay

import (
	"math"
	"sort"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/winchance"
)

// Starter is one matchup starter with provider totals used to anchor the replay.
type Starter struct {
	Key             string
	TeamB           bool
	AthleteID       string
	ProTeam         string
	StartPoints     float64
	StartTouchdowns int
	StartFieldGoals int
	FinalPoints     float64
	Projected       *float64
}

// Game contains one NFL game and its plays in chronological order.
type Game struct {
	HomeTeam string
	AwayTeam string
	Plays    []fantasy.NFLPlay
}

// Input contains the starters, game timelines, and league scoring rules to replay.
type Input struct {
	Starters []Starter
	Games    []Game
	Rules    []fantasy.ScoringRule
	From     time.Time
	To       time.Time
}

// Change is one starter's cumulative points and scoring counts around a minute bucket.
type Change struct {
	Key                               string
	At                                time.Time
	PointsBefore, PointsAfter         float64
	TouchdownsBefore, TouchdownsAfter int
	FieldGoalsBefore, FieldGoalsAfter int
}

// Sample is the matchup's estimated win chance after a minute bucket.
type Sample struct {
	At       time.Time
	ScoreA   float64
	ScoreB   float64
	Estimate winchance.Result
}

// Timeline contains reconstructed matchup samples and classified-player inputs.
type Timeline struct {
	Samples []Sample
	Changes []Change
}

type gameTimeline struct {
	game     Game
	firstAt  time.Time
	lastAt   time.Time
	lastPlay fantasy.NFLPlay
}

type playerState struct {
	points     float64
	touchdowns int
	fieldGoals int
}

// Build replays plays into per-minute point changes and win-chance samples anchored to provider totals.
func Build(in Input) Timeline {
	games, earliestAt, latestAt := prepareGames(in.Games)
	if earliestAt.IsZero() || latestAt.IsZero() {
		if in.To.IsZero() {
			return Timeline{}
		}
		if earliestAt.IsZero() {
			earliestAt = in.To
		}
		if latestAt.IsZero() {
			latestAt = in.To
		}
	}

	starterGames := make([]int, len(in.Starters))
	for i, starter := range in.Starters {
		starterGames[i] = gameForStarter(starter, games)
	}

	events, credited, earnedPoints := creditWindowPlays(in, games, starterGames)
	anchorResiduals(in, games, starterGames, latestAt, events, credited, earnedPoints)
	timeline := replayChangesAndSamples(in, games, starterGames, latestAt, earliestAt, events)
	timeline.Samples = appendSample(timeline.Samples, finalSample(in, games, starterGames, latestAt))
	return timeline
}

// creditWindowPlays computes per-bucket credits and totals for plays in the replay window.
func creditWindowPlays(
	in Input,
	games []gameTimeline,
	starterGames []int,
) (map[time.Time]map[int]playerDelta, []bool, []float64) {
	events := map[time.Time]map[int]playerDelta{}
	credited := make([]bool, len(in.Starters))
	earnedPoints := make([]float64, len(in.Starters))
	rulePoints := scoringPoints(in.Rules)
	for gameIndex, game := range games {
		for _, play := range game.game.Plays {
			if !playWithinWindow(play.At, in.From, in.To) {
				continue
			}
			bucket := replayBucket(play.At, in.From)
			for starterIndex, starter := range in.Starters {
				if starterGames[starterIndex] != gameIndex || starter.AthleteID == "" {
					continue
				}
				delta := creditPlay(play, starter.AthleteID, rulePoints)
				if !delta.credited {
					continue
				}
				credited[starterIndex] = true
				if events[bucket] == nil {
					events[bucket] = map[int]playerDelta{}
				}
				events[bucket][starterIndex] = addDelta(events[bucket][starterIndex], delta)
			}
		}
	}
	for _, bucket := range sortedBuckets(events) {
		for starterIndex := range in.Starters {
			earnedPoints[starterIndex] += events[bucket][starterIndex].points
		}
	}
	return events, credited, earnedPoints
}

// anchorResiduals align replayed credits with each starter's provider total.
func anchorResiduals(
	in Input,
	games []gameTimeline,
	starterGames []int,
	latestAt time.Time,
	events map[time.Time]map[int]playerDelta,
	credited []bool,
	earnedPoints []float64,
) {
	for starterIndex, starter := range in.Starters {
		residual := starter.FinalPoints - starter.StartPoints - earnedPoints[starterIndex]
		if credited[starterIndex] && math.Abs(residual) < 0.005 {
			continue
		}
		if !credited[starterIndex] && residual == 0 {
			continue
		}
		anchorAt := latestAt
		gameIndex := starterGames[starterIndex]
		if !in.To.IsZero() {
			anchorAt = in.To
			if credited[starterIndex] && gameIndex >= 0 &&
				gameEndedWithinWindow(games[gameIndex], in.From, in.To) {
				anchorAt = games[gameIndex].lastAt
			}
		} else if credited[starterIndex] && gameIndex >= 0 && !games[gameIndex].lastAt.IsZero() {
			anchorAt = games[gameIndex].lastAt
		}
		bucket := replayBucket(anchorAt, in.From)
		if events[bucket] == nil {
			events[bucket] = map[int]playerDelta{}
		}
		delta := events[bucket][starterIndex]
		delta.adjustment += residual
		events[bucket][starterIndex] = delta
	}
}

// replayChangesAndSamples converts credited buckets into cumulative changes and samples.
func replayChangesAndSamples(
	in Input,
	games []gameTimeline,
	starterGames []int,
	latestAt, earliestAt time.Time,
	events map[time.Time]map[int]playerDelta,
) Timeline {
	result := Timeline{}
	states := make([]playerState, len(in.Starters))
	for i, starter := range in.Starters {
		states[i] = playerState{
			points:     starter.StartPoints,
			touchdowns: starter.StartTouchdowns,
			fieldGoals: starter.StartFieldGoals,
		}
	}
	if in.From.IsZero() {
		result.Samples = appendSample(
			result.Samples, replaySample(in, games, starterGames, latestAt, minuteBucket(earliestAt), 0, 0, true, false),
		)
	}
	for _, bucket := range sortedBuckets(events) {
		bucketChanged := false
		starterIndices := make([]int, 0, len(events[bucket]))
		for starterIndex := range events[bucket] {
			starterIndices = append(starterIndices, starterIndex)
		}
		sort.Ints(starterIndices)
		for _, starterIndex := range starterIndices {
			delta := events[bucket][starterIndex]
			before := states[starterIndex]
			creditedAfter := playerState{
				points:     before.points + delta.points,
				touchdowns: before.touchdowns + delta.touchdowns,
				fieldGoals: before.fieldGoals + delta.fieldGoals,
			}
			pointsBefore, pointsAfter := round4(before.points), round4(creditedAfter.points)
			if pointsBefore != pointsAfter ||
				before.touchdowns != creditedAfter.touchdowns ||
				before.fieldGoals != creditedAfter.fieldGoals {
				result.Changes = append(result.Changes, Change{
					Key: in.Starters[starterIndex].Key, At: bucket,
					PointsBefore: pointsBefore, PointsAfter: pointsAfter,
					TouchdownsBefore: before.touchdowns, TouchdownsAfter: creditedAfter.touchdowns,
					FieldGoalsBefore: before.fieldGoals, FieldGoalsAfter: creditedAfter.fieldGoals,
				})
			}
			after := creditedAfter
			after.points += delta.adjustment
			if round4(before.points) != round4(after.points) ||
				before.touchdowns != after.touchdowns ||
				before.fieldGoals != after.fieldGoals {
				bucketChanged = true
			}
			states[starterIndex] = after
		}
		if bucketChanged {
			scoreA, scoreB := sideScores(in.Starters, states)
			result.Samples = appendSample(result.Samples, replaySample(
				in, games, starterGames, latestAt, bucket, scoreA, scoreB, false, false,
			))
		}
	}
	return result
}

// finalSample records the provider totals as the replay's final score.
func finalSample(
	in Input,
	games []gameTimeline,
	starterGames []int,
	latestAt time.Time,
) Sample {
	finalA, finalB := finalScores(in.Starters)
	finalAt := minuteBucket(latestAt)
	finalEstimate := true
	if !in.To.IsZero() {
		finalAt = in.To
		finalEstimate = false
	}
	return replaySample(
		in, games, starterGames, latestAt, finalAt, finalA, finalB, false, finalEstimate,
	)
}

func sideScores(starters []Starter, states []playerState) (float64, float64) {
	var scoreA, scoreB float64
	for i, starter := range starters {
		if starter.TeamB {
			scoreB += states[i].points
		} else {
			scoreA += states[i].points
		}
	}
	return round4(scoreA), round4(scoreB)
}

func finalScores(starters []Starter) (float64, float64) {
	var scoreA, scoreB float64
	for _, starter := range starters {
		if starter.TeamB {
			scoreB += starter.FinalPoints
		} else {
			scoreA += starter.FinalPoints
		}
	}
	return round4(scoreA), round4(scoreB)
}

func appendSample(samples []Sample, sample Sample) []Sample {
	if len(samples) > 0 && sameSample(samples[len(samples)-1], sample) {
		return samples
	}
	return append(samples, sample)
}

func sameSample(a, b Sample) bool {
	return round4(a.ScoreA) == round4(b.ScoreA) &&
		round4(a.ScoreB) == round4(b.ScoreB) &&
		round4(a.Estimate.ProjectedA) == round4(b.Estimate.ProjectedA) &&
		round4(a.Estimate.ProjectedB) == round4(b.Estimate.ProjectedB) &&
		round4(a.Estimate.WinChanceA) == round4(b.Estimate.WinChanceA)
}
