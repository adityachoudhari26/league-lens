// Package database owns pooled queries and the transaction boundary for application writes.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adityachoudhari26/league-lens/internal/store"
)

type DB struct {
	pool    *pgxpool.Pool
	queries *store.Queries
}

// Open configures a pool without connecting eagerly or running migrations.
// Ping is used by readiness to report availability independently of liveness.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure database: %w", err)
	}
	return &DB{pool: pool, queries: store.New(pool)}, nil
}

func (db *DB) Close() { db.pool.Close() }

func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

func (db *DB) Queries() *store.Queries { return db.queries }

// InTx commits only if fn succeeds and the context remains valid. Errors and
// panics roll back all writes. Use the provided queries for every operation in
// fn; using db.Queries() would escape the transaction. Do not retain them after
// fn returns. Callbacks are never retried, and nested transactions are unsupported.
func (db *DB) InTx(ctx context.Context, fn func(*store.Queries) error) error {
	return db.inTx(ctx, pgx.TxOptions{}, fn)
}

// InReadTx gives all queries in one dashboard request a consistent database view.
// PostgreSQL also rejects writes within this transaction.
func (db *DB) InReadTx(ctx context.Context, fn func(*store.Queries) error) error {
	return db.inTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

func (db *DB) inTx(ctx context.Context, options pgx.TxOptions, fn func(*store.Queries) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := db.pool.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Cleanup must still run if the request was canceled or fn panicked.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx) // After commit this returns pgx.ErrTxClosed.
	}()
	if err := fn(db.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
