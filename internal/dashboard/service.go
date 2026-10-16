// Package dashboard serves owner-scoped reads of imported data.
package dashboard

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerprofile"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type Store interface {
	InReadTx(context.Context, func(*store.Queries) error) error
	InTx(context.Context, func(*store.Queries) error) error
}

type WinChanceBackfill interface {
	RequestWinChanceBackfill(pgtype.UUID) bool
}

type Service struct {
	db         Store
	ownerEmail string
	profiles   playerprofile.Source
	backfill   WinChanceBackfill
	scoreboard LiveScoreboard
	now        func() time.Time

	liveGamesMu        sync.Mutex
	liveGamesCache     []fantasy.NFLGameStatus
	liveGamesFetchedAt time.Time
	liveGamesCached    bool
}

// New builds the dashboard service. Nil optional sources disable their fetches
// or finished-matchup backfills.
func New(db Store, ownerEmail string, profiles playerprofile.Source, backfill WinChanceBackfill, scoreboard LiveScoreboard) *Service {
	return &Service{
		db: db, ownerEmail: ownerEmail, profiles: profiles, backfill: backfill,
		scoreboard: scoreboard, now: time.Now,
	}
}

// read resolves the configured local owner and runs scoped queries within one
// consistent read-only transaction, bounded by a five-second deadline.
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

// write resolves the configured local owner and runs scoped queries in a
// transaction bounded by a five-second deadline.
func (s *Service) write(ctx context.Context, fn func(context.Context, *store.Queries, pgtype.UUID) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.ownerEmail)
		if err != nil {
			return err
		}
		return fn(ctx, q, owner.ID)
	})
	return writeError(err)
}

// readError maps storage failures to RPC codes without exposing database details.
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

func writeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, context.Canceled):
		return readError(err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, "write timed out")
	default:
		return connect.NewError(connect.CodeInternal, "could not save dashboard data")
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
