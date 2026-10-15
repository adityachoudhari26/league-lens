package replay

import (
	"math"
	"sort"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/winchance"
)

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

// starterRemaining estimates how much of a starter's game remains at a replay sample.
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
