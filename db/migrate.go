// Package db owns SQL migrations; generated sqlc bindings will live in internal/store.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies an explicit command. Application startup never changes the schema.
func Migrate(ctx context.Context, conn *sql.DB, direction string) error {
	files, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, files)
	if err != nil {
		return err
	}
	switch direction {
	case "up":
		_, err = provider.Up(ctx)
	case "down":
		_, err = provider.Down(ctx)
	default:
		return fmt.Errorf("unknown migration direction %q; use up or down", direction)
	}
	return err
}
