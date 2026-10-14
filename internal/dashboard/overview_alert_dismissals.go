package dashboard

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// DismissLineupAlert hides a current alert for the configured local owner.
func (s *Service) DismissLineupAlert(ctx context.Context, req *v1.DismissLineupAlertRequest) (*v1.DismissLineupAlertResponse, error) {
	if err := validateAlertFingerprint(req.GetFingerprint()); err != nil {
		return nil, err
	}
	err := s.write(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		return q.DismissAlert(ctx, store.DismissAlertParams{UserID: owner, Fingerprint: req.GetFingerprint()})
	})
	if err != nil {
		return nil, err
	}
	return &v1.DismissLineupAlertResponse{}, nil
}

// RestoreLineupAlert removes a dismissal for the configured local owner.
func (s *Service) RestoreLineupAlert(ctx context.Context, req *v1.RestoreLineupAlertRequest) (*v1.RestoreLineupAlertResponse, error) {
	if err := validateAlertFingerprint(req.GetFingerprint()); err != nil {
		return nil, err
	}
	err := s.write(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		return q.RestoreAlert(ctx, store.RestoreAlertParams{UserID: owner, Fingerprint: req.GetFingerprint()})
	})
	if err != nil {
		return nil, err
	}
	return &v1.RestoreLineupAlertResponse{}, nil
}

func validateAlertFingerprint(fingerprint string) error {
	if fingerprint == "" || len(fingerprint) > 200 {
		return connect.NewError(connect.CodeInvalidArgument, "a valid alert fingerprint is required")
	}
	return nil
}
