// Package importer coordinates provider reads and atomic local persistence.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Service struct {
	DB             *database.DB
	Source         fantasy.Provider
	Catalog        playerids.Catalog
	CatalogVersion string
}
type Request struct {
	OwnerEmail, ExternalAccountID string
	Selection                     fantasy.ConnectionSelection
}
type Result struct {
	Teams, Players, MatchupPeriods int
	SyncID                         pgtype.UUID
}

// importTarget is the durable context shared by both import transactions.
type importTarget struct {
	user       store.User
	connection store.Connection
	league     store.LeagueSeason
	run        store.SyncRun
	selection  fantasy.ConnectionSelection
}

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func (s *Service) Import(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.OwnerEmail) == "" || request.ExternalAccountID == "" || request.Selection.League.ExternalID == "" {
		return Result{}, fmt.Errorf("owner, external account, and league selection required")
	}
	header, err := s.Source.DescribeLeague(ctx, request.Selection.League.ExternalID)
	if err != nil {
		return Result{}, err
	}
	if header.Ref != request.Selection.League {
		return Result{}, fmt.Errorf("league selection changed")
	}
	target, err := s.startImport(ctx, request, header)
	if err != nil {
		return Result{}, err
	}
	result := Result{SyncID: target.run.ID}
	data, err := s.Source.FetchLeague(ctx, header)
	if err != nil {
		return result, s.recordFailure(target.run.ID, err, "provider_fetch_failed")
	}
	if err = validateData(data, header, request.Selection); err != nil {
		return result, s.recordFailure(target.run.ID, err, "invalid_provider_data")
	}
	result, err = s.saveImport(ctx, target, data)
	if err != nil {
		return result, s.recordFailure(target.run.ID, err, "import_write_failed")
	}
	return result, nil
}

// Establish the import target and durable run before bulk provider reads.
func (s *Service) startImport(ctx context.Context, request Request, header fantasy.League) (importTarget, error) {
	var target importTarget
	settings, err := json.Marshal(header.Settings)
	if err != nil {
		return target, err
	}
	err = s.DB.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		user, err := ensureOwner(ctx, queries, request.OwnerEmail)
		if err != nil {
			return err
		}
		connection, err := ensureProviderConnection(ctx, queries, user.ID, string(header.Ref.Provider), request.ExternalAccountID)
		if err != nil {
			return err
		}
		league, err := queries.EnsureLeagueSeason(ctx, store.EnsureLeagueSeasonParams{Provider: string(header.Ref.Provider), ExternalLeagueID: header.Ref.ExternalID, Season: int32(header.Ref.Season), Name: header.Name, SettingsJson: settings})
		if err != nil {
			return err
		}
		if err = queries.AttachLeague(ctx, store.AttachLeagueParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, UserID: user.ID, Provider: string(header.Ref.Provider)}); err != nil {
			return err
		}
		kind := "initial"
		if connection.Status == "active" {
			kind = "refresh"
		}
		run, err := queries.StartSync(ctx, store.StartSyncParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, Kind: kind})
		target = importTarget{user: user, connection: connection, league: league, run: run, selection: request.Selection}
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

func ensureProviderConnection(ctx context.Context, queries *store.Queries, userID pgtype.UUID, provider, externalID string) (store.Connection, error) {
	connection, err := queries.FindProviderConnection(ctx, store.FindProviderConnectionParams{UserID: userID, Provider: provider, ExternalUserID: text(externalID)})
	if errors.Is(err, pgx.ErrNoRows) {
		connection, err = queries.CreateConnection(ctx, store.CreateConnectionParams{UserID: userID, Provider: provider, ExternalUserID: text(externalID)})
	}
	if err == nil && connection.Status == "disabled" {
		err = fmt.Errorf("connection is disabled")
	}
	return connection, err
}

func (s *Service) saveImport(ctx context.Context, target importTarget, data fantasy.LeagueData) (Result, error) {
	result := Result{SyncID: target.run.ID}
	settings, err := json.Marshal(data.League.Settings)
	if err != nil {
		return result, err
	}
	err = s.DB.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		if err := rejectSupersededImport(ctx, queries, target); err != nil {
			return err
		}
		// Persist metadata from the fetched payload, not an earlier description.
		league := target.league
		var err error
		if _, err = queries.UpsertLeagueSeason(ctx, store.UpsertLeagueSeasonParams{Provider: league.Provider, ExternalLeagueID: league.ExternalLeagueID, Season: league.Season, Name: data.League.Name, SettingsJson: settings}); err != nil {
			return err
		}
		writer := newLeagueWriter(s, queries, target, data)
		result.Teams, result.Players, result.MatchupPeriods, err = writer.write(ctx)
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

// RecordedSyncError is an import failure saved on its sync run, carrying the
// stable, credential-free code that was recorded.
type RecordedSyncError struct {
	Code string
	Err  error
}

func (e *RecordedSyncError) Error() string { return e.Err.Error() }

func (e *RecordedSyncError) Unwrap() error { return e.Err }

func (s *Service) recordFailure(runID pgtype.UUID, cause error, code string) error {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.DB.Queries().FailSync(cleanup, store.FailSyncParams{ID: runID, ErrorCode: text(code)})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("record failed sync: %w", err))
	}
	return &RecordedSyncError{Code: code, Err: cause}
}
