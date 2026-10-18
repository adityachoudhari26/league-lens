package livescore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/adityachoudhari26/league-lens/internal/winchance"
)

// saveWinChanceSample records the matchup's current win estimate when it differs from the latest saved sample.
func (l *Loop) saveWinChanceSample(
	ctx context.Context,
	queries *store.Queries,
	candidate store.ListLiveMatchupCandidatesRow,
	scoreboard fantasy.NFLScoreboard,
	scoreA, scoreB *float64,
) error {
	if !candidate.TeamBID.Valid {
		return nil
	}

	sideA, sideB, err := loadWinChanceSides(
		ctx, queries, candidate, newLiveGameClock(scoreboard.Games), scoreA, scoreB,
	)
	if err != nil {
		return err
	}
	sample, err := newWinChanceSample(
		candidate.MatchupID, l.now(), sideA.Score, sideB.Score, winchance.Estimate(sideA, sideB),
	)
	if err != nil {
		return err
	}
	changed, err := winChanceSampleChanged(ctx, queries, sample)
	if err != nil || !changed {
		return err
	}
	return queries.InsertWinChanceSample(ctx, sample)
}

// liveGameClock indexes scoreboard games by NFL team and tracks whether all games are final.
type liveGameClock struct {
	gamesByTeam map[string]fantasy.NFLGameStatus
	allFinal    bool
}

// newLiveGameClock indexes each game's home and away teams and detects an all-final scoreboard.
func newLiveGameClock(games []fantasy.NFLGameStatus) liveGameClock {
	clock := liveGameClock{
		gamesByTeam: make(map[string]fantasy.NFLGameStatus, len(games)*2),
		allFinal:    len(games) > 0,
	}
	for _, game := range games {
		if game.State != fantasy.NFLGameFinal {
			clock.allFinal = false
		}
		if game.HomeTeam != "" {
			clock.gamesByTeam[game.HomeTeam] = game
		}
		if game.AwayTeam != "" {
			clock.gamesByTeam[game.AwayTeam] = game
		}
	}
	return clock
}

// starterRemaining returns game time for known teams, treating byes as complete and unknown teams conservatively.
func (c liveGameClock) starterRemaining(proTeam string) float64 {
	if proTeam == "" {
		if c.allFinal {
			return 0
		}
		return 1
	}
	game, found := c.gamesByTeam[proTeam]
	if !found {
		return 0
	}
	return winchance.GameRemaining(game)
}

// loadWinChanceSides loads starters and groups their projections by the stored matchup sides.
func loadWinChanceSides(
	ctx context.Context,
	queries *store.Queries,
	candidate store.ListLiveMatchupCandidatesRow,
	clock liveGameClock,
	scoreA, scoreB *float64,
) (winchance.Side, winchance.Side, error) {
	rows, err := queries.ListLiveMatchupStarters(ctx, candidate.MatchupID)
	if err != nil {
		return winchance.Side{}, winchance.Side{}, err
	}
	sideA := winchance.Side{Score: scoreOrZero(scoreA)}
	sideB := winchance.Side{Score: scoreOrZero(scoreB)}
	for _, row := range rows {
		starter, err := winChanceStarter(row, clock)
		if err != nil {
			return winchance.Side{}, winchance.Side{}, err
		}
		switch row.TeamID {
		case candidate.TeamAID:
			sideA.Starters = append(sideA.Starters, starter)
		case candidate.TeamBID:
			sideB.Starters = append(sideB.Starters, starter)
		default:
			return winchance.Side{}, winchance.Side{}, fmt.Errorf("live matchup starter team does not match stored matchup")
		}
	}
	return sideA, sideB, nil
}

// winChanceStarter converts a stored starter row into the model input for its remaining game time.
func winChanceStarter(row store.ListLiveMatchupStartersRow, clock liveGameClock) (winchance.Starter, error) {
	projection, err := liveProjection(row.ProjectedPoints)
	if err != nil {
		return winchance.Starter{}, err
	}
	return winchance.Starter{
		Projected: projection,
		Remaining: clock.starterRemaining(row.ProTeam),
	}, nil
}

// newWinChanceSample converts estimated values to four-decimal database parameters.
func newWinChanceSample(
	matchupID pgtype.UUID,
	observedAt time.Time,
	scoreA, scoreB float64,
	estimate winchance.Result,
) (store.InsertWinChanceSampleParams, error) {
	teamAWinChance, err := fourDecimalNumeric(estimate.WinChanceA)
	if err != nil {
		return store.InsertWinChanceSampleParams{}, err
	}
	numericScoreA, err := fourDecimalNumeric(scoreA)
	if err != nil {
		return store.InsertWinChanceSampleParams{}, err
	}
	numericScoreB, err := fourDecimalNumeric(scoreB)
	if err != nil {
		return store.InsertWinChanceSampleParams{}, err
	}
	projectedA, err := fourDecimalNumeric(estimate.ProjectedA)
	if err != nil {
		return store.InsertWinChanceSampleParams{}, err
	}
	projectedB, err := fourDecimalNumeric(estimate.ProjectedB)
	if err != nil {
		return store.InsertWinChanceSampleParams{}, err
	}
	return store.InsertWinChanceSampleParams{
		MatchupID:      matchupID,
		ObservedAt:     pgtype.Timestamptz{Time: observedAt, Valid: true},
		TeamAWinChance: teamAWinChance,
		ScoreA:         numericScoreA,
		ScoreB:         numericScoreB,
		ProjectedA:     projectedA,
		ProjectedB:     projectedB,
	}, nil
}

// winChanceSampleChanged reports whether a sample differs from the latest persisted values.
func winChanceSampleChanged(
	ctx context.Context,
	queries *store.Queries,
	sample store.InsertWinChanceSampleParams,
) (bool, error) {
	previous, err := queries.LatestWinChanceSample(ctx, sample.MatchupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	matches, err := sameWinChanceSample(previous, sample)
	return !matches, err
}

// sameWinChanceSample compares the five values used to deduplicate samples.
func sameWinChanceSample(previous store.LatestWinChanceSampleRow, current store.InsertWinChanceSampleParams) (bool, error) {
	previousValues := []pgtype.Numeric{
		previous.TeamAWinChance, previous.ScoreA, previous.ScoreB, previous.ProjectedA, previous.ProjectedB,
	}
	currentValues := []pgtype.Numeric{
		current.TeamAWinChance, current.ScoreA, current.ScoreB, current.ProjectedA, current.ProjectedB,
	}
	for i, previousValue := range previousValues {
		previousFloat, err := previousValue.Float64Value()
		if err != nil {
			return false, err
		}
		currentFloat, err := currentValues[i].Float64Value()
		if err != nil {
			return false, err
		}
		if !previousFloat.Valid || !currentFloat.Valid || previousFloat.Float64 != currentFloat.Float64 {
			return false, nil
		}
	}
	return true, nil
}

// liveProjection converts a valid numeric projection to the model's float input.
func liveProjection(points pgtype.Numeric) (*float64, error) {
	if !points.Valid {
		return nil, nil
	}
	value, err := points.Float64Value()
	if err != nil {
		return nil, err
	}
	return &value.Float64, nil
}

// scoreOrZero treats a missing matchup score as zero for win-chance calculations.
func scoreOrZero(score *float64) float64 {
	if score == nil {
		return 0
	}
	return *score
}
