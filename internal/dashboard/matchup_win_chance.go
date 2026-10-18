package dashboard

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/keymoments"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// GetMatchupWinChance returns the owner's saved win-chance samples and classified key moments.
func (s *Service) GetMatchupWinChance(
	ctx context.Context,
	request *v1.GetMatchupWinChanceRequest,
) (*v1.GetMatchupWinChanceResponse, error) {
	matchupID, err := parseID(request.MatchupId)
	if err != nil {
		return nil, err
	}

	response := &v1.GetMatchupWinChanceResponse{}
	requestBackfill := false
	err = s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		matchup, err := q.MatchupWinChanceOwnerMatchup(ctx, store.MatchupWinChanceOwnerMatchupParams{
			MatchupID: matchupID,
			UserID:    owner,
		})
		if err != nil {
			return err
		}
		if matchup.IsBye {
			return nil
		}
		response.WinChance, err = matchupWinChanceMessage(ctx, q, owner, matchup.ID, matchup.OwnerIsTeamB)
		if err != nil {
			return err
		}
		if matchup.Status != string(fantasy.MatchupFinal) {
			if response.WinChance == nil || len(response.WinChance.Samples) == 0 {
				requestBackfill = true
				return nil
			}
			requestBackfill, err = q.MatchupWinChanceStartsLate(ctx, store.MatchupWinChanceStartsLateParams{
				MatchupID: matchup.ID,
				Now:       pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
			})
			return err
		}
		matchesFinal, err := matchupWinChanceMatchesFinal(
			response.WinChance, matchup.ScoreA, matchup.ScoreB, matchup.OwnerIsTeamB,
		)
		if err != nil {
			return err
		}
		requestBackfill = response.WinChance == nil || !matchesFinal
		return nil
	})
	if err != nil {
		return nil, err
	}
	if requestBackfill && s.backfill != nil {
		response.Building = s.backfill.RequestWinChanceBackfill(matchupID)
	}
	return response, nil
}

// matchupWinChanceMatchesFinal reports whether the latest owner-oriented sample has the saved final score.
func matchupWinChanceMatchesFinal(
	winChance *v1.MatchupWinChance,
	scoreA, scoreB pgtype.Numeric,
	ownerIsTeamB bool,
) (bool, error) {
	if winChance == nil || len(winChance.Samples) == 0 || !scoreA.Valid || !scoreB.Valid {
		return false, nil
	}
	finalA, err := requiredNumericValue(scoreA)
	if err != nil {
		return false, err
	}
	finalB, err := requiredNumericValue(scoreB)
	if err != nil {
		return false, err
	}
	if ownerIsTeamB {
		finalA, finalB = finalB, finalA
	}
	latest := winChance.Samples[len(winChance.Samples)-1]
	return math.Round(latest.TeamScore*10000) == math.Round(finalA*10000) &&
		math.Round(latest.OpponentScore*10000) == math.Round(finalB*10000), nil
}

// matchupWinChanceMessage loads a matchup's saved samples and classified starter moments.
func matchupWinChanceMessage(
	ctx context.Context,
	q *store.Queries,
	owner, matchupID pgtype.UUID,
	ownerIsTeamB bool,
) (*v1.MatchupWinChance, error) {
	sampleRows, err := q.MatchupDetailWinChanceSamples(ctx, store.MatchupDetailWinChanceSamplesParams{
		MatchupID: matchupID,
		UserID:    owner,
	})
	if err != nil {
		return nil, err
	}
	pointChangeRows, err := q.MatchupDetailPointChanges(ctx, store.MatchupDetailPointChangesParams{
		MatchupID: matchupID,
		UserID:    owner,
	})
	if err != nil {
		return nil, err
	}
	pointChangeRows, err = mergeSplitScoringChanges(pointChangeRows)
	if err != nil {
		return nil, err
	}

	message := &v1.MatchupWinChance{}
	for _, row := range sampleRows {
		sample, err := winChanceSampleMessage(row, ownerIsTeamB)
		if err != nil {
			return nil, err
		}
		message.Samples = append(message.Samples, sample)
	}
	for _, row := range pointChangeRows {
		moment, classified, err := keyMomentMessage(row)
		if err != nil {
			return nil, err
		}
		if classified {
			message.Moments = append(message.Moments, moment)
		}
	}
	if len(message.Samples) == 0 && len(message.Moments) == 0 {
		return nil, nil
	}
	return message, nil
}

const (
	splitScoringChangeWindow = 3 * time.Minute
	unchangedPointsTolerance = 0.005
)

type pointChangePlayer struct {
	teamID, playerID pgtype.UUID
}

// mergeSplitScoringChanges joins a provider's count-only and points-only updates for one score.
func mergeSplitScoringChanges(rows []store.MatchupDetailPointChangesRow) ([]store.MatchupDetailPointChangesRow, error) {
	merged := make([]store.MatchupDetailPointChangesRow, 0, len(rows))
	for _, playerRows := range pointChangesByPlayer(rows) {
		playerMerged, err := mergePlayerSplitScoring(playerRows)
		if err != nil {
			return nil, err
		}
		merged = append(merged, playerMerged...)
	}
	sortPointChanges(merged)
	return merged, nil
}

// pointChangesByPlayer groups rows by player while preserving their input time order.
func pointChangesByPlayer(rows []store.MatchupDetailPointChangesRow) map[pointChangePlayer][]store.MatchupDetailPointChangesRow {
	groups := make(map[pointChangePlayer][]store.MatchupDetailPointChangesRow, len(rows))
	for _, row := range rows {
		key := pointChangePlayer{teamID: row.TeamID, playerID: row.PlayerID}
		groups[key] = append(groups[key], row)
	}
	return groups
}

// mergePlayerSplitScoring merges adjacent eligible pairs without chaining them.
func mergePlayerSplitScoring(rows []store.MatchupDetailPointChangesRow) ([]store.MatchupDetailPointChangesRow, error) {
	merged := make([]store.MatchupDetailPointChangesRow, 0, len(rows))
	for index := 0; index < len(rows); index++ {
		if index+1 >= len(rows) {
			merged = append(merged, rows[index])
			continue
		}
		mergedRow, paired, err := mergeSplitScoringPair(rows[index], rows[index+1])
		if err != nil {
			return nil, err
		}
		if !paired {
			merged = append(merged, rows[index])
			continue
		}
		merged = append(merged, mergedRow)
		index++
	}
	return merged, nil
}

// mergeSplitScoringPair joins two close rows when exactly one is count-only.
// The merged row uses the points update's time, when the win-chance swing occurred.
func mergeSplitScoringPair(
	earlier, later store.MatchupDetailPointChangesRow,
) (store.MatchupDetailPointChangesRow, bool, error) {
	if !withinSplitScoringWindow(earlier, later) {
		return store.MatchupDetailPointChangesRow{}, false, nil
	}
	earlierCountOnly, err := isCountOnlyChange(earlier)
	if err != nil {
		return store.MatchupDetailPointChangesRow{}, false, err
	}
	laterCountOnly, err := isCountOnlyChange(later)
	if err != nil {
		return store.MatchupDetailPointChangesRow{}, false, err
	}
	if earlierCountOnly == laterCountOnly {
		return store.MatchupDetailPointChangesRow{}, false, nil
	}
	merged := earlier
	if earlierCountOnly {
		merged.ObservedAt = later.ObservedAt
	}
	merged.PointsAfter = later.PointsAfter
	merged.TouchdownsAfter = later.TouchdownsAfter
	merged.FieldGoalsAfter = later.FieldGoalsAfter
	return merged, true, nil
}

// withinSplitScoringWindow reports whether both timestamps are valid and close enough to merge.
func withinSplitScoringWindow(earlier, later store.MatchupDetailPointChangesRow) bool {
	if !earlier.ObservedAt.Valid || !later.ObservedAt.Valid {
		return false
	}
	elapsed := later.ObservedAt.Time.Sub(earlier.ObservedAt.Time)
	return elapsed >= 0 && elapsed <= splitScoringChangeWindow
}

// isCountOnlyChange reports a scoring-count increase without a meaningful points change.
func isCountOnlyChange(row store.MatchupDetailPointChangesRow) (bool, error) {
	pointsBefore, err := requiredNumericValue(row.PointsBefore)
	if err != nil {
		return false, err
	}
	pointsAfter, err := requiredNumericValue(row.PointsAfter)
	if err != nil {
		return false, err
	}
	return math.Abs(pointsAfter-pointsBefore) < unchangedPointsTolerance &&
		(countIncreased(row.TouchdownsBefore, row.TouchdownsAfter) ||
			countIncreased(row.FieldGoalsBefore, row.FieldGoalsAfter)), nil
}

// countIncreased reports whether a known touchdown or field-goal count increased.
func countIncreased(before, after pgtype.Int4) bool {
	return before.Valid && after.Valid && after.Int32 > before.Int32
}

// sortPointChanges orders rows by observed time, then team and player ID.
func sortPointChanges(rows []store.MatchupDetailPointChangesRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if !left.ObservedAt.Time.Equal(right.ObservedAt.Time) {
			return left.ObservedAt.Time.Before(right.ObservedAt.Time)
		}
		if left.TeamID.String() != right.TeamID.String() {
			return left.TeamID.String() < right.TeamID.String()
		}
		return left.PlayerID.String() < right.PlayerID.String()
	})
}

// winChanceSampleMessage orients stored team-A values around the owner and rounds flipped probabilities.
func winChanceSampleMessage(
	row store.MatchupDetailWinChanceSamplesRow,
	ownerIsTeamB bool,
) (*v1.WinChanceSample, error) {
	winChance, err := requiredNumericValue(row.TeamAWinChance)
	if err != nil {
		return nil, err
	}
	scoreA, err := requiredNumericValue(row.ScoreA)
	if err != nil {
		return nil, err
	}
	scoreB, err := requiredNumericValue(row.ScoreB)
	if err != nil {
		return nil, err
	}
	projectedA, err := requiredNumericValue(row.ProjectedA)
	if err != nil {
		return nil, err
	}
	projectedB, err := requiredNumericValue(row.ProjectedB)
	if err != nil {
		return nil, err
	}
	if ownerIsTeamB {
		winChance = math.Round((1-winChance)*10000) / 10000
		scoreA, scoreB = scoreB, scoreA
		projectedA, projectedB = projectedB, projectedA
	}
	return &v1.WinChanceSample{
		ObservedAt:        timestamp(row.ObservedAt),
		TeamWinChance:     winChance,
		TeamScore:         scoreA,
		OpponentScore:     scoreB,
		TeamProjected:     projectedA,
		OpponentProjected: projectedB,
	}, nil
}

// keyMomentMessage classifies a stored point change and omits changes that are not key moments.
func keyMomentMessage(row store.MatchupDetailPointChangesRow) (*v1.KeyMoment, bool, error) {
	pointsBefore, err := requiredNumericValue(row.PointsBefore)
	if err != nil {
		return nil, false, err
	}
	pointsAfter, err := requiredNumericValue(row.PointsAfter)
	if err != nil {
		return nil, false, err
	}
	moment, classified := keymoments.Classify(keymoments.PointChange{
		PointsBefore:     pointsBefore,
		PointsAfter:      pointsAfter,
		TouchdownsBefore: optionalInt4Value(row.TouchdownsBefore),
		TouchdownsAfter:  optionalInt4Value(row.TouchdownsAfter),
		FieldGoalsBefore: optionalInt4Value(row.FieldGoalsBefore),
		FieldGoalsAfter:  optionalInt4Value(row.FieldGoalsAfter),
	})
	if !classified {
		return nil, false, nil
	}
	return &v1.KeyMoment{
		ObservedAt: timestamp(row.ObservedAt),
		TeamId:     row.TeamID.String(),
		PlayerId:   row.PlayerID.String(),
		PlayerName: row.DisplayName,
		Kind:       keyMomentKindMessage(moment.Kind),
		Points:     moment.Points,
	}, true, nil
}

// keyMomentKindMessage maps a classified moment kind to its API enum.
func keyMomentKindMessage(kind keymoments.Kind) v1.KeyMomentKind {
	switch kind {
	case keymoments.KindTouchdown:
		return v1.KeyMomentKind_KEY_MOMENT_KIND_TOUCHDOWN
	case keymoments.KindFieldGoal:
		return v1.KeyMomentKind_KEY_MOMENT_KIND_FIELD_GOAL
	case keymoments.KindPlay:
		return v1.KeyMomentKind_KEY_MOMENT_KIND_PLAY
	default:
		return v1.KeyMomentKind_KEY_MOMENT_KIND_UNSPECIFIED
	}
}

// requiredNumericValue converts a non-null database numeric using the shared optional score conversion.
func requiredNumericValue(value pgtype.Numeric) (float64, error) {
	result, err := optionalScore(value)
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, errors.New("required matchup numeric is null")
	}
	return *result, nil
}

// optionalInt4Value maps an unknown database count to a nil model count.
func optionalInt4Value(value pgtype.Int4) *int {
	if !value.Valid {
		return nil
	}
	count := int(value.Int32)
	return &count
}
