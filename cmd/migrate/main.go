package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/adityachoudhari26/league-lens/db"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "down") {
		slog.Error("usage: migrate up|down (down rolls back one migration)")
		os.Exit(2)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(2)
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		slog.Error("invalid DATABASE_URL")
		os.Exit(2)
	}
	conn := stdlib.OpenDB(*cfg)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, conn, os.Args[1]); err != nil {
		// Driver errors may include connection strings; keep command output credential-free.
		slog.Error("migration failed; verify database availability, permissions, and migration SQL")
		os.Exit(1)
	}
	slog.Info("migration complete", "direction", os.Args[1])
}
