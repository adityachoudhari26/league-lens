// Package importer coordinates provider reads and atomic local persistence.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
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
	// League optionally carries a description fetched moments ago by the caller.
	// When its Ref matches Selection.League, Begin uses it instead of describing again.
	League fantasy.League
	// FullHistory reads every week; the CLI import uses it as the rebuild path.
	FullHistory bool
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

func optionalText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

// Started is an import whose league is attached and whose sync run is open
// (status running). Finish completes it with the same provider Source.
type Started struct {
	header      fantasy.League
	target      importTarget
	fullHistory bool
}

// LeagueSeasonID is the attached league's ID, the key sync status uses.
func (s Started) LeagueSeasonID() pgtype.UUID { return s.target.league.ID }

// Begin does the quick part of an import: checks the request, describes the
// league, confirms it still matches the selection, then attaches it and opens
// a sync run. No bulk provider reads happen here.
func (s *Service) Begin(ctx context.Context, request Request) (Started, error) {
	if strings.TrimSpace(request.OwnerEmail) == "" || request.ExternalAccountID == "" || request.Selection.League.ExternalID == "" {
		return Started{}, fmt.Errorf("owner, external account, and league selection required")
	}
	header := request.League
	if header.Ref != request.Selection.League {
		var err error
		header, err = s.Source.DescribeLeague(ctx, request.Selection.League.ExternalID)
		if err != nil {
			return Started{}, err
		}
	}
	if header.Ref != request.Selection.League {
		return Started{}, fmt.Errorf("league selection changed")
	}
	target, err := s.startImport(ctx, request, header)
	if err != nil {
		return Started{}, err
	}
	return Started{header: header, target: target, fullHistory: request.FullHistory}, nil
}

// Finish does the slow part: fetches the full league, validates it, and saves
// it atomically. Each failure is recorded on the sync run as a RecordedSyncError
// with a stable code (provider_fetch_failed, invalid_provider_data, import_write_failed).
func (s *Service) Finish(ctx context.Context, started Started) (Result, error) {
	target := started.target
	result := Result{SyncID: target.run.ID}
	skipWeeks := s.weeksToSkip(ctx, started)
	data, err := s.Source.FetchLeague(ctx, started.header, fantasy.FetchRequest{SkipWeeks: skipWeeks})
	if err != nil {
		return result, s.recordFailure(target.run.ID, err, "provider_fetch_failed")
	}
	if err = validateData(data, started.header, target.selection); err != nil {
		return result, s.recordFailure(target.run.ID, err, "invalid_provider_data")
	}
	result, err = s.saveImport(ctx, target, data)
	if err != nil {
		return result, s.recordFailure(target.run.ID, err, "import_write_failed")
	}
	return result, nil
}

// weeksToSkip lists the settled weeks Finish may skip re-reading. A week is
// settled when every matchup covering it is final AND every team has a
// provider_history snapshot for it. The newest settled week is always re-read
// as the stat-correction window; providers still read the current week even if
// it is listed. A read failure falls back to a full read rather than failing
// the import.
func (s *Service) weeksToSkip(ctx context.Context, started Started) []fantasy.ScoringPeriod {
	if started.fullHistory {
		return nil
	}
	settled, err := s.DB.Queries().ListSettledWeeks(ctx, started.target.league.ID)
	if err != nil {
		slog.Warn("settled-week check failed; reading full history", "league_season_id", started.target.league.ID, "error", err)
		return nil
	}
	if len(settled) <= 1 {
		return nil
	}
	skip := make([]fantasy.ScoringPeriod, 0, len(settled)-1)
	for _, week := range settled[:len(settled)-1] {
		skip = append(skip, fantasy.ScoringPeriod(week))
	}
	return skip
}

// Import runs Begin then Finish in one call.
func (s *Service) Import(ctx context.Context, request Request) (Result, error) {
	started, err := s.Begin(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return s.Finish(ctx, started)
}

// Abandon closes a begun import's sync run as failed without fetching, used
// when another sync of the same league is already running.
func (s *Service) Abandon(started Started, code string) error {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.DB.Queries().FailSync(cleanup, store.FailSyncParams{ID: started.target.run.ID, ErrorCode: text(code)})
	return err
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
		league, err := queries.EnsureLeagueSeason(ctx, store.EnsureLeagueSeasonParams{
			Provider:         string(header.Ref.Provider),
			ExternalLeagueID: header.Ref.ExternalID,
			Season:           int32(header.Ref.Season),
			Name:             header.Name,
			SettingsJson:     settings,
		})
		if err != nil {
			return err
		}
		if err = queries.AttachLeague(ctx, store.AttachLeagueParams{
			ConnectionID:   connection.ID,
			LeagueSeasonID: league.ID,
			UserID:         user.ID,
			Provider:       string(header.Ref.Provider),
		}); err != nil {
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
	connection, err := queries.FindProviderConnection(ctx, store.FindProviderConnectionParams{
		UserID:         userID,
		Provider:       provider,
		ExternalUserID: text(externalID),
	})
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
		if _, err = queries.UpsertLeagueSeason(ctx, store.UpsertLeagueSeasonParams{
			Provider:         league.Provider,
			ExternalLeagueID: league.ExternalLeagueID,
			Season:           league.Season,
			Name:             data.League.Name,
			SettingsJson:     settings,
		}); err != nil {
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
	rowsAffected, err := queries.SetConnectionStatusForUser(ctx, store.SetConnectionStatusForUserParams{
		ID: target.connection.ID, UserID: target.user.ID, Status: "active",
	})
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return fmt.Errorf("connection status was not updated")
	}
	completedRun, err := queries.CompleteSync(ctx, target.run.ID)
	if err != nil {
		return err
	}
	return queries.MarkLeagueSynced(ctx, store.MarkLeagueSyncedParams{
		ConnectionID:         target.connection.ID,
		LeagueSeasonID:       target.league.ID,
		LastSuccessfulSyncAt: completedRun.CompletedAt,
	})
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
