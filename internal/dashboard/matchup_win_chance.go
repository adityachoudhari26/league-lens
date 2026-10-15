package dashboard

import (
	"context"
	"errors"
	"math"

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
		if err != nil || matchup.Status != string(fantasy.MatchupFinal) {
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
