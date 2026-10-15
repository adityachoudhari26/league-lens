package leaguesync

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// liveBaselineMaxAge resets the baseline after a restart, sleep, or outage gap instead of creating one big change.
const liveBaselineMaxAge = 5 * time.Minute

type liveLineupObservationKey struct {
	teamID   pgtype.UUID
	playerID pgtype.UUID
}

// loadLiveLineupObservations reads the last live observation for each stored matchup player.
func loadLiveLineupObservations(
	ctx context.Context,
	queries *store.Queries,
	matchupID pgtype.UUID,
) (map[liveLineupObservationKey]store.ListLiveLineupObservationsRow, error) {
	rows, err := queries.ListLiveLineupObservations(ctx, matchupID)
	if err != nil {
		return nil, err
	}
	observations := make(map[liveLineupObservationKey]store.ListLiveLineupObservationsRow, len(rows))
	for _, row := range rows {
		key := liveLineupObservationKey{teamID: row.TeamID, playerID: row.PlayerID}
		observations[key] = row
	}
	return observations, nil
}

// saveLiveLineupEntry updates a provider entry, records eligible starter changes, and refreshes its baseline.
func (r *Runner) saveLiveLineupEntry(
	ctx context.Context,
	queries *store.Queries,
	matchupID, teamID pgtype.UUID,
	provider string,
	entry fantasy.LineupEntry,
	observations map[liveLineupObservationKey]store.ListLiveLineupObservationsRow,
) (bool, error) {
	points, err := liveScore(entry.Points)
	if err != nil {
		return false, err
	}
	touchdowns, fieldGoals := liveCounts(entry.Scoring)
	updatedRows, err := queries.UpdateLiveLineupEntry(ctx, store.UpdateLiveLineupEntryParams{
		Points: points, Touchdowns: touchdowns, FieldGoals: fieldGoals,
		MatchupID: matchupID, TeamID: teamID, Provider: provider, ExternalPlayerID: entry.PlayerID,
	})
	if err != nil {
		return false, err
	}
	if len(updatedRows) == 0 {
		return false, nil
	}

	observedAt := r.now()
	for _, after := range updatedRows {
		key := liveLineupObservationKey{teamID: teamID, playerID: after.PlayerID}
		baseline, hasBaseline := observations[key]
		if after.Starter && hasBaseline && observedAt.Sub(baseline.ObservedAt.Time) <= liveBaselineMaxAge {
			changed, err := lineupValuesChanged(baseline, after)
			if err != nil {
				return false, err
			}
			if changed {
				if err := insertLineupPointChange(ctx, queries, matchupID, teamID, baseline, after, observedAt); err != nil {
					return false, err
				}
			}
		}

		updatedObservation := store.ListLiveLineupObservationsRow{
			TeamID: teamID, PlayerID: after.PlayerID, Points: after.Points,
			Touchdowns: after.Touchdowns, FieldGoals: after.FieldGoals,
			ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true},
		}
		if err := queries.UpsertLiveLineupObservation(ctx, store.UpsertLiveLineupObservationParams{
			MatchupID: matchupID, TeamID: teamID, PlayerID: after.PlayerID,
			Points: after.Points, Touchdowns: after.Touchdowns, FieldGoals: after.FieldGoals,
			ObservedAt: updatedObservation.ObservedAt,
		}); err != nil {
			return false, err
		}
		observations[key] = updatedObservation
	}
	return true, nil
}

// lineupValuesChanged detects four-decimal point changes and changes between known scoring counts.
func lineupValuesChanged(
	before store.ListLiveLineupObservationsRow,
	after store.UpdateLiveLineupEntryRow,
) (bool, error) {
	pointsBefore, err := lineupPointsOrZero(before.Points)
	if err != nil {
		return false, err
	}
	pointsAfter, err := lineupPointsOrZero(after.Points)
	if err != nil {
		return false, err
	}
	return pointsBefore != pointsAfter ||
		knownCountChanged(before.Touchdowns, after.Touchdowns) ||
		knownCountChanged(before.FieldGoals, after.FieldGoals), nil
}

// knownCountChanged reports a count change only when both observations know the value.
func knownCountChanged(before, after pgtype.Int4) bool {
	return before.Valid && after.Valid && before.Int32 != after.Int32
}

// insertLineupPointChange persists a starter change using the live observation as its before-value.
func insertLineupPointChange(
	ctx context.Context,
	queries *store.Queries,
	matchupID, teamID pgtype.UUID,
	before store.ListLiveLineupObservationsRow,
	after store.UpdateLiveLineupEntryRow,
	observedAt time.Time,
) error {
	pointsBefore, err := lineupPointsOrZero(before.Points)
	if err != nil {
		return err
	}
	pointsAfter, err := lineupPointsOrZero(after.Points)
	if err != nil {
		return err
	}
	numericPointsBefore, err := liveScore(&pointsBefore)
	if err != nil {
		return err
	}
	numericPointsAfter, err := liveScore(&pointsAfter)
	if err != nil {
		return err
	}
	return queries.InsertLineupPointChange(ctx, store.InsertLineupPointChangeParams{
		MatchupID: matchupID, TeamID: teamID, PlayerID: after.PlayerID,
		ObservedAt:       pgtype.Timestamptz{Time: observedAt, Valid: true},
		PointsBefore:     numericPointsBefore,
		PointsAfter:      numericPointsAfter,
		TouchdownsBefore: before.Touchdowns,
		TouchdownsAfter:  after.Touchdowns,
		FieldGoalsBefore: before.FieldGoals,
		FieldGoalsAfter:  after.FieldGoals,
	})
}

// lineupPointsOrZero treats an unreported score as zero for a stored point change.
func lineupPointsOrZero(points pgtype.Numeric) (float64, error) {
	if !points.Valid {
		return 0, nil
	}
	value, err := points.Float64Value()
	if err != nil {
		return 0, err
	}
	return value.Float64, nil
}
