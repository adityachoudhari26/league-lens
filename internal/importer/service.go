// Package importer coordinates provider reads and atomic local persistence.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Source interface {
	Header(context.Context, string, string) (sleeper.Header, error)
	Fetch(context.Context, sleeper.Header) (sleeper.Data, error)
}
type Service struct {
	DB             *database.DB
	Source         Source
	Catalog        playerids.Catalog
	CatalogVersion string
}
type Request struct{ OwnerEmail, Username, LeagueID string }
type Result struct {
	Teams, Players, Weeks int
	SyncID                pgtype.UUID
}

// importTarget is the durable context shared by both import transactions.
type importTarget struct {
	user       store.User
	connection store.Connection
	league     store.LeagueSeason
	run        store.SyncRun
	name       string
	settings   []byte
}

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func (s *Service) Import(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.OwnerEmail) == "" || request.Username == "" || request.LeagueID == "" {
		return Result{}, fmt.Errorf("owner, username, and league ID required")
	}
	header, err := s.Source.Header(ctx, request.Username, request.LeagueID)
	if err != nil {
		return Result{}, err
	}
	target, err := s.startImport(ctx, request.OwnerEmail, header)
	if err != nil {
		return Result{}, err
	}
	result := Result{SyncID: target.run.ID}
	data, err := s.Source.Fetch(ctx, header)
	if err != nil {
		return result, s.recordFailure(target.run.ID, err, "provider_fetch_failed")
	}
	result, err = s.saveImport(ctx, target, data)
	if err != nil {
		return result, s.recordFailure(target.run.ID, err, "import_write_failed")
	}
	return result, nil
}

// Establish the import target and durable run before bulk provider reads.
func (s *Service) startImport(ctx context.Context, email string, header sleeper.Header) (importTarget, error) {
	var target importTarget
	year, err := strconv.Atoi(header.League.Season)
	if err != nil {
		return target, err
	}
	settings, err := json.Marshal(header.League)
	if err != nil {
		return target, err
	}
	err = s.DB.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		user, err := ensureOwner(ctx, queries, email)
		if err != nil {
			return err
		}
		connection, err := ensureSleeperConnection(ctx, queries, user.ID, header.User.ID)
		if err != nil {
			return err
		}
		league, err := queries.EnsureLeagueSeason(ctx, store.EnsureLeagueSeasonParams{Provider: "sleeper", ExternalLeagueID: header.League.ID, Season: int32(year), Name: header.League.Name, SettingsJson: settings})
		if err != nil {
			return err
		}
		if err = queries.AttachLeague(ctx, store.AttachLeagueParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, UserID: user.ID, Provider: "sleeper"}); err != nil {
			return err
		}
		kind := "initial"
		if connection.Status == "active" {
			kind = "refresh"
		}
		run, err := queries.StartSync(ctx, store.StartSyncParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, Kind: kind})
		target = importTarget{user: user, connection: connection, league: league, run: run, name: header.League.Name, settings: settings}
		return err
	})
	return target, err
}

func ensureOwner(ctx context.Context, queries *store.Queries, email string) (store.User, error) {
	email = strings.TrimSpace(email)
	owner, err := queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.CreateUser(ctx, email)
	}
	return owner, err
}

func ensureSleeperConnection(ctx context.Context, queries *store.Queries, userID pgtype.UUID, externalID string) (store.Connection, error) {
	connection, err := queries.FindSleeperConnection(ctx, store.FindSleeperConnectionParams{UserID: userID, ExternalUserID: text(externalID)})
	if errors.Is(err, pgx.ErrNoRows) {
		connection, err = queries.CreateConnection(ctx, store.CreateConnectionParams{UserID: userID, Provider: "sleeper", ExternalUserID: text(externalID)})
	}
	if err == nil && connection.Status == "disabled" {
		err = fmt.Errorf("connection is disabled")
	}
	return connection, err
}

func (s *Service) saveImport(ctx context.Context, target importTarget, data sleeper.Data) (Result, error) {
	result := Result{SyncID: target.run.ID}
	err := s.DB.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		if err := rejectSupersededImport(ctx, queries, target); err != nil {
			return err
		}
		// startImport prepared this metadata from the same provider header.
		league := target.league
		var err error
		if _, err = queries.UpsertLeagueSeason(ctx, store.UpsertLeagueSeasonParams{Provider: "sleeper", ExternalLeagueID: league.ExternalLeagueID, Season: league.Season, Name: target.name, SettingsJson: target.settings}); err != nil {
			return err
		}
		writer := newLeagueWriter(s, queries, target, data)
		result.Teams, result.Players, result.Weeks, err = writer.write(ctx)
		if err != nil {
			return err
		}
		return completeImport(ctx, queries, target)
	})
	return result, err
}

func rejectSupersededImport(ctx context.Context, queries *store.Queries, target importTarget) error {
	newerImportCompleted, err := queries.HasNewerSuccessfulSync(ctx, store.HasNewerSuccessfulSyncParams{LeagueSeasonID: target.league.ID, ID: target.run.ID})
	if err != nil {
		return err
	}
	if newerImportCompleted {
		return fmt.Errorf("a newer import already completed; retry")
	}
	return nil
}

func completeImport(ctx context.Context, queries *store.Queries, target importTarget) error {
	if _, err := queries.UpdateConnectionForUser(ctx, store.UpdateConnectionForUserParams{ID: target.connection.ID, UserID: target.user.ID, Status: "active"}); err != nil {
		return err
	}
	completedRun, err := queries.CompleteSync(ctx, target.run.ID)
	if err != nil {
		return err
	}
	return queries.MarkLeagueSynced(ctx, store.MarkLeagueSyncedParams{ConnectionID: target.connection.ID, LeagueSeasonID: target.league.ID, LastSuccessfulSyncAt: completedRun.CompletedAt})
}

func (s *Service) recordFailure(runID pgtype.UUID, cause error, code string) error {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.DB.Queries().FailSync(cleanup, store.FailSyncParams{ID: runID, ErrorCode: text(code)})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("record failed sync: %w", err))
	}
	return cause
}
