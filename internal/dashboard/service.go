// Package dashboard serves owner-scoped reads of imported data.
package dashboard

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Reader interface {
	InReadTx(context.Context, func(*store.Queries) error) error
}
type Service struct {
	db         Reader
	ownerEmail string
}

func New(db Reader, ownerEmail string) *Service { return &Service{db: db, ownerEmail: ownerEmail} }

func (s *Service) read(ctx context.Context, fn func(context.Context, *store.Queries, pgtype.UUID) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.db.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.ownerEmail)
		if err != nil {
			return err
		}
		return fn(ctx, q, owner.ID)
	})
	return readError(err)
}
func readError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return connect.NewError(connect.CodeNotFound, "requested data not found")
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, "read timed out")
	default:
		return connect.NewError(connect.CodeInternal, "could not read dashboard data")
	}
}
func parseID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if len(value) != 36 || id.Scan(value) != nil || !id.Valid {
		return id, connect.NewError(connect.CodeInvalidArgument, "a valid resource UUID is required")
	}
	return id, nil
}
func validatePeriod(value int32) error {
	if value < 1 || value > 100 {
		return connect.NewError(connect.CodeInvalidArgument, "period must be between 1 and 100")
	}
	return nil
}
