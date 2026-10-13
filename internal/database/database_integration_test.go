//go:build integration

package database_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func testDatabase(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL must point to a disposable database")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	admin := stdlib.OpenDB(*cfg)
	schema := fmt.Sprintf("tx_test_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	cfg.RuntimeParams["search_path"] = schema
	migrationDB := stdlib.OpenDB(*cfg)
	defer migrationDB.Close()
	if err := migrations.Migrate(ctx, migrationDB, "up"); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	params := parsed.Query()
	params.Set("search_path", schema)
	params.Set("pool_max_conns", "1")
	parsed.RawQuery = params.Encode()
	result, err := database.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(result.Close)
	return result
}

func TestTransactions(t *testing.T) {
	db := testDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("commit multiple typed writes", func(t *testing.T) {
		err := db.InTx(ctx, func(q *store.Queries) error {
			user, err := q.CreateUser(ctx, "committed@example.test")
			if err != nil {
				return err
			}
			_, err = q.CreateConnection(ctx, store.CreateConnectionParams{UserID: user.ID, Provider: "sleeper"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		user, err := db.Queries().GetUserByEmail(ctx, "COMMITTED@example.test")
		if err != nil || user.Email != "committed@example.test" {
			t.Fatalf("committed record unavailable: %v", err)
		}
	})
	assertAbsent := func(email string) {
		t.Helper()
		_, err := db.Queries().GetUserByEmail(ctx, email)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected rolled back user %s, got %v", email, err)
		}
	}
	t.Run("callback error rolls back and preserves cause", func(t *testing.T) {
		cause := errors.New("import rejected")
		err := db.InTx(ctx, func(q *store.Queries) error {
			if _, err := q.CreateUser(ctx, "rejected@example.test"); err != nil {
				return err
			}
			return cause
		})
		if !errors.Is(err, cause) {
			t.Fatalf("callback cause lost: %v", err)
		}
		assertAbsent("rejected@example.test")
	})
	t.Run("SQL failure rolls back earlier writes", func(t *testing.T) {
		err := db.InTx(ctx, func(q *store.Queries) error {
			if _, err := q.CreateUser(ctx, "duplicate@example.test"); err != nil {
				return err
			}
			_, err := q.CreateUser(ctx, "duplicate@example.test")
			return err
		})
		if err == nil {
			t.Fatal("duplicate insert succeeded")
		}
		assertAbsent("duplicate@example.test")
	})
	t.Run("aborted transaction cannot report commit success", func(t *testing.T) {
		err := db.InTx(ctx, func(q *store.Queries) error {
			if _, err := q.CreateUser(ctx, "aborted@example.test"); err != nil {
				return err
			}
			// Simulate a caller accidentally swallowing a statement error.
			_, _ = q.CreateUser(ctx, "aborted@example.test")
			return nil
		})
		if !errors.Is(err, pgx.ErrTxCommitRollback) {
			t.Fatalf("commit failure not propagated: %v", err)
		}
		assertAbsent("aborted@example.test")
	})
	t.Run("panic rolls back and propagates", func(t *testing.T) {
		marker := errors.New("panic marker")
		func() {
			defer func() {
				if recover() != marker {
					t.Error("panic was swallowed or changed")
				}
			}()
			_ = db.InTx(ctx, func(q *store.Queries) error {
				if _, err := q.CreateUser(ctx, "panic@example.test"); err != nil {
					t.Fatal(err)
				}
				panic(marker)
			})
		}()
		assertAbsent("panic@example.test")
	})
	t.Run("cancellation cannot commit and releases connection", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		defer cancel()
		err := db.InTx(canceled, func(q *store.Queries) error {
			if _, err := q.CreateUser(canceled, "canceled@example.test"); err != nil {
				return err
			}
			cancel()
			return nil
		})
		if err == nil {
			t.Fatal("canceled transaction committed")
		}
		assertAbsent("canceled@example.test")
		if err := db.Ping(ctx); err != nil {
			t.Fatalf("pool connection not reusable: %v", err)
		}
	})
	t.Run("already canceled does not execute callback", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		ran := false
		err := db.InTx(canceled, func(*store.Queries) error { ran = true; return nil })
		if err == nil || ran {
			t.Fatalf("error=%v, callback ran=%v", err, ran)
		}
	})
}
