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

type playerDelta struct {
	points     float64
	touchdowns int
	fieldGoals int
	credited   bool
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

	rulePoints := scoringPoints(in.Rules)
	starterGames := make([]int, len(in.Starters))
	for i, starter := range in.Starters {
		starterGames[i] = gameForStarter(starter, games)
	}

	events := map[time.Time]map[int]playerDelta{}
	credited := make([]bool, len(in.Starters))
	earnedPoints := make([]float64, len(in.Starters))
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
		delta.points += residual
		events[bucket][starterIndex] = delta
	}

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

	buckets := sortedBuckets(events)
	for _, bucket := range buckets {
		changesBefore := len(result.Changes)
		starterIndices := make([]int, 0, len(events[bucket]))
		for starterIndex := range events[bucket] {
			starterIndices = append(starterIndices, starterIndex)
		}
		sort.Ints(starterIndices)
		for _, starterIndex := range starterIndices {
			delta := events[bucket][starterIndex]
			before := states[starterIndex]
			after := playerState{
				points:     before.points + delta.points,
				touchdowns: before.touchdowns + delta.touchdowns,
				fieldGoals: before.fieldGoals + delta.fieldGoals,
			}
			pointsBefore, pointsAfter := round4(before.points), round4(after.points)
			if pointsBefore != pointsAfter || before.touchdowns != after.touchdowns || before.fieldGoals != after.fieldGoals {
				result.Changes = append(result.Changes, Change{
					Key: in.Starters[starterIndex].Key, At: bucket,
					PointsBefore: pointsBefore, PointsAfter: pointsAfter,
					TouchdownsBefore: before.touchdowns, TouchdownsAfter: after.touchdowns,
					FieldGoalsBefore: before.fieldGoals, FieldGoalsAfter: after.fieldGoals,
				})
			}
			states[starterIndex] = after
		}
		if len(result.Changes) > changesBefore {
			scoreA, scoreB := sideScores(in.Starters, states)
			result.Samples = appendSample(result.Samples, replaySample(
				in, games, starterGames, latestAt, bucket, scoreA, scoreB, false, false,
			))
		}
	}

	finalA, finalB := finalScores(in.Starters)
	finalAt := minuteBucket(latestAt)
	finalEstimate := true
	if !in.To.IsZero() {
		finalAt = in.To
		finalEstimate = false
	}
	result.Samples = appendSample(result.Samples, replaySample(
		in, games, starterGames, latestAt, finalAt, finalA, finalB, false, finalEstimate,
	))
	return result
}

func playWithinWindow(at, from, to time.Time) bool {
	return (from.IsZero() || at.After(from)) && (to.IsZero() || !at.After(to))
}

func replayBucket(at, from time.Time) time.Time {
	bucket := minuteBucket(at)
	if !from.IsZero() && !bucket.After(from) {
		return minuteBucket(from).Add(time.Minute)
	}
	return bucket
}

func gameEndedWithinWindow(game gameTimeline, from, to time.Time) bool {
	return game.lastPlay.Kind == fantasy.NFLPlayEndOfGame &&
		(from.IsZero() || game.lastAt.After(from)) &&
		(to.IsZero() || !game.lastAt.After(to))
}

func prepareGames(games []Game) ([]gameTimeline, time.Time, time.Time) {
	result := make([]gameTimeline, 0, len(games))
	var earliest, latest time.Time
	for _, game := range games {
		plays := append([]fantasy.NFLPlay(nil), game.Plays...)
		sort.SliceStable(plays, func(i, j int) bool { return plays[i].At.Before(plays[j].At) })
		if len(plays) == 0 {
			result = append(result, gameTimeline{game: Game{
				HomeTeam: game.HomeTeam, AwayTeam: game.AwayTeam,
			}})
			continue
		}
		firstAt, lastAt := plays[0].At, plays[len(plays)-1].At
		if earliest.IsZero() || firstAt.Before(earliest) {
			earliest = firstAt
		}
		if latest.IsZero() || lastAt.After(latest) {
			latest = lastAt
		}
		result = append(result, gameTimeline{
			game:    Game{HomeTeam: game.HomeTeam, AwayTeam: game.AwayTeam, Plays: plays},
			firstAt: firstAt, lastAt: lastAt, lastPlay: plays[len(plays)-1],
		})
	}
	return result, earliest, latest
}

func scoringPoints(rules []fantasy.ScoringRule) map[fantasy.Stat]float64 {
	points := make(map[fantasy.Stat]float64, len(rules))
	for _, rule := range rules {
		perUnits := rule.PerUnits
		if perUnits == 0 {
			perUnits = 1
		}
		points[rule.Stat] += rule.Points / perUnits
	}
	return points
}

func gameForStarter(starter Starter, games []gameTimeline) int {
	if starter.AthleteID != "" {
		for gameIndex, game := range games {
			for _, play := range game.game.Plays {
				for _, participant := range play.Participants {
					if participant.AthleteID == starter.AthleteID {
						return gameIndex
					}
				}
			}
		}
	}
	if starter.ProTeam != "" {
		for gameIndex, game := range games {
			if game.game.HomeTeam == starter.ProTeam || game.game.AwayTeam == starter.ProTeam {
				return gameIndex
			}
		}
	}
	return -1
}

func creditPlay(play fantasy.NFLPlay, athleteID string, rulePoints map[fantasy.Stat]float64) playerDelta {
	delta := playerDelta{}
	switch play.Kind {
	case fantasy.NFLPlayRush, fantasy.NFLPlayRushingTouchdown:
		rusher := participantID(play, "rusher")
		if rusher == athleteID {
			addStat(&delta, fantasy.StatRushingYards, float64(play.Yards), rulePoints)
		}
		if play.Kind == fantasy.NFLPlayRushingTouchdown {
			scorer := participantID(play, "scorer")
			if scorer == "" {
				scorer = rusher
			}
			if scorer == athleteID {
				addStat(&delta, fantasy.StatRushingTouchdowns, 1, rulePoints)
				delta.touchdowns++
			}
		}
	case fantasy.NFLPlayPassReception, fantasy.NFLPlayPassingTouchdown:
		passer := participantID(play, "passer")
		receiver := participantID(play, "receiver")
		if receiver == "" {
			receiver = participantID(play, "scorer")
		}
		if passer == athleteID {
			addStat(&delta, fantasy.StatPassingYards, float64(play.Yards), rulePoints)
		}
		if receiver == athleteID {
			addStat(&delta, fantasy.StatReceivingYards, float64(play.Yards), rulePoints)
			addStat(&delta, fantasy.StatReceptions, 1, rulePoints)
		}
		if play.Kind == fantasy.NFLPlayPassingTouchdown {
			if passer == athleteID {
				addStat(&delta, fantasy.StatPassingTouchdowns, 1, rulePoints)
				delta.touchdowns++
			}
			scorer := participantID(play, "scorer")
			if scorer == "" {
				scorer = receiver
			}
			if scorer == athleteID {
				addStat(&delta, fantasy.StatReceivingTouchdowns, 1, rulePoints)
				delta.touchdowns++
			}
		}
	case fantasy.NFLPlayInterception:
		if participantID(play, "passer") == athleteID {
			addStat(&delta, fantasy.StatInterceptions, 1, rulePoints)
		}
	case fantasy.NFLPlayFieldGoalGood:
		if participantID(play, "kicker") == athleteID {
			delta.points += 3
			delta.fieldGoals++
			delta.credited = true
		}
	}

	if play.ExtraPointGood &&
		(play.Kind == fantasy.NFLPlayRushingTouchdown || play.Kind == fantasy.NFLPlayPassingTouchdown) {
		kicker := participantID(play, "kicker")
		if kicker == "" {
			kicker = participantID(play, "patScorer")
		}
		if kicker == athleteID {
			delta.points++
			delta.credited = true
		}
	}
	return delta
}

func participantID(play fantasy.NFLPlay, role string) string {
	for _, participant := range play.Participants {
		if participant.Role == role {
			return participant.AthleteID
		}
	}
	return ""
}

func addStat(delta *playerDelta, stat fantasy.Stat, value float64, rulePoints map[fantasy.Stat]float64) {
	delta.points += value * rulePoints[stat]
	delta.credited = true
}

func addDelta(before, after playerDelta) playerDelta {
	return playerDelta{
		points:     before.points + after.points,
		touchdowns: before.touchdowns + after.touchdowns,
		fieldGoals: before.fieldGoals + after.fieldGoals,
		credited:   before.credited || after.credited,
	}
}

func replaySample(
	in Input,
	games []gameTimeline,
	starterGames []int,
	latestAt, at time.Time,
	scoreA, scoreB float64,
	pregame, final bool,
) Sample {
	sideA := winchance.Side{Score: scoreA}
	sideB := winchance.Side{Score: scoreB}
	for i, starter := range in.Starters {
		remaining := 0.0
		if pregame || !final {
			remaining = 1
		}
		if !pregame && !final {
			remaining = starterRemaining(starterGames[i], games, latestAt, at)
		}
		item := winchance.Starter{Projected: starter.Projected, Remaining: remaining}
		if starter.TeamB {
			sideB.Starters = append(sideB.Starters, item)
		} else {
			sideA.Starters = append(sideA.Starters, item)
		}
	}
	estimate := winchance.Estimate(sideA, sideB)
	estimate.WinChanceA = round4(estimate.WinChanceA)
	estimate.ProjectedA = round4(estimate.ProjectedA)
	estimate.ProjectedB = round4(estimate.ProjectedB)
	return Sample{At: at, ScoreA: round4(scoreA), ScoreB: round4(scoreB), Estimate: estimate}
}

func starterRemaining(gameIndex int, games []gameTimeline, latestAt, at time.Time) float64 {
	if gameIndex < 0 {
		return remainingUntil(at, latestAt)
	}
	game := games[gameIndex]
	if len(game.game.Plays) == 0 {
		return 1
	}
	bucket := minuteBucket(at)
	firstBucket, lastBucket := minuteBucket(game.firstAt), minuteBucket(game.lastAt)
	if bucket.Before(firstBucket) {
		return 1
	}
	if bucket.After(lastBucket) ||
		(bucket.Equal(lastBucket) && game.lastPlay.Kind == fantasy.NFLPlayEndOfGame) {
		return 0
	}
	var latestPlay *fantasy.NFLPlay
	for i := range game.game.Plays {
		play := &game.game.Plays[i]
		if minuteBucket(play.At).After(bucket) {
			break
		}
		latestPlay = play
	}
	if latestPlay == nil {
		return 1
	}
	return winchance.GameRemaining(fantasy.NFLGameStatus{
		State: fantasy.NFLGameLive, Period: latestPlay.Period, Clock: latestPlay.Clock,
	})
}

func remainingUntil(at, latestAt time.Time) float64 {
	if latestAt.IsZero() || at.Before(minuteBucket(latestAt)) {
		return 1
	}
	return 0
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

func minuteBucket(at time.Time) time.Time {
	return at.UTC().Truncate(time.Minute)
}

func sortedBuckets(events map[time.Time]map[int]playerDelta) []time.Time {
	buckets := make([]time.Time, 0, len(events))
	for bucket := range events {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Before(buckets[j]) })
	return buckets
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}
