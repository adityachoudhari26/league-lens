package dashboard

import (
	"context"
	"errors"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetTeamRoster returns the latest observed roster unless a scoring week is requested.
// A missing snapshot is absent, while a known empty roster retains its snapshot.
func (s *Service) GetTeamRoster(ctx context.Context, request *v1.GetTeamRosterRequest) (*v1.GetTeamRosterResponse, error) {
	id, err := parseID(request.TeamId)
	if err != nil {
		return nil, err
	}
	var period int32
	if request.ScoringPeriod != nil {
		if err = validatePeriod(*request.ScoringPeriod); err != nil {
			return nil, err
		}
		period = *request.ScoringPeriod
	}
	response := &v1.GetTeamRosterResponse{}
	err = s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		if _, err := q.DashboardTeam(ctx, store.DashboardTeamParams{ID: id, UserID: owner}); err != nil {
			return err
		}
		snapshot, err := q.DashboardRoster(ctx, store.DashboardRosterParams{TeamID: id, UserID: owner, Period: period})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		entries, err := q.ListSnapshotRosterForUser(ctx, store.ListSnapshotRosterForUserParams{UserID: owner, SnapshotID: snapshot.ID})
		if err != nil {
			return err
		}
		response.Snapshot, err = rosterMessage(snapshot, entries)
		return err
	})
	return response, err
}
