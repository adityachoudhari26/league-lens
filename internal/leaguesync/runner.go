package leaguesync

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

const (
	// StaleAfter is how old saved data may be before a load-triggered sync refreshes it.
	StaleAfter = 15 * time.Minute

	leagueSyncTimeout = 5 * time.Minute
	targetReadTimeout = 5 * time.Second
)

// Runner refreshes the local owner's leagues in the background through the
// shared importer. Each league has at most one sync in flight. In-flight and
// failure state live in memory, so they do not survive a restart.
type Runner struct {
	db          *database.DB
	ownerEmail  string
	sources     SourceFactory
	catalogPath string
	now         func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	running  map[string]bool
	failures map[string]failure
}

// failure is the most recent failed sync of a league since its last success.
type failure struct {
	code string
	at   time.Time
}

// New creates a runner with the path to the optional player mapping cache.
func New(db *database.DB, ownerEmail string, sources SourceFactory, catalogPath string) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		db:          db,
		ownerEmail:  ownerEmail,
		sources:     sources,
		catalogPath: catalogPath,
		now:         time.Now,
		ctx:         ctx,
		cancel:      cancel,
		running:     map[string]bool{},
		failures:    map[string]failure{},
	}
}

// Close cancels in-flight syncs and waits for them to record their outcome.
func (r *Runner) Close() {
	r.cancel()
	r.wg.Wait()
}

// Start begins background syncs and returns each league's status immediately.
// Statuses are read before launch so claimed leagues always report syncing.
// With onlyStale, leagues whose data is still fresh are skipped.
func (r *Runner) Start(ctx context.Context, onlyStale bool) ([]Status, error) {
	targets, err := r.listTargets(ctx)
	if err != nil {
		return nil, err
	}

	claimedTargets := r.claimTargets(targets, onlyStale)
	statuses := r.statuses(targets)
	r.launch(claimedTargets)
	return statuses, nil
}

// Status reports each league's sync state without starting any sync.
func (r *Runner) Status(ctx context.Context) ([]Status, error) {
	targets, err := r.listTargets(ctx)
	if err != nil {
		return nil, err
	}
	return r.statuses(targets), nil
}

func (r *Runner) listTargets(ctx context.Context) ([]Target, error) {
	_, targets, err := r.listTargetsWithOwner(ctx)
	return targets, err
}

func (r *Runner) listTargetsWithOwner(ctx context.Context) (pgtype.UUID, []Target, error) {
	ctx, cancel := context.WithTimeout(ctx, targetReadTimeout)
	defer cancel()

	queries := r.db.Queries()
	owner, err := queries.GetUserByEmail(ctx, r.ownerEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil, nil
	}
	if err != nil {
		return pgtype.UUID{}, nil, err
	}

	rows, err := queries.ListSyncTargets(ctx, owner.ID)
	if err != nil {
		return pgtype.UUID{}, nil, err
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, targetFromRow(row))
	}
	return owner.ID, targets, nil
}

// claimTargets marks the targets this request will sync, skipping leagues
// that are already syncing so concurrent requests never duplicate work.
func (r *Runner) claimTargets(targets []Target, onlyStale bool) []Target {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	var claimed []Target
	for _, target := range targets {
		if r.running[target.ID] || (onlyStale && !isStale(target, now)) {
			continue
		}
		r.running[target.ID] = true
		claimed = append(claimed, target)
	}
	return claimed
}

// isStale reports whether a league has never synced or its saved data is older than StaleAfter.
func isStale(target Target, now time.Time) bool {
	return target.LastSuccessfulSyncAt.IsZero() || now.Sub(target.LastSuccessfulSyncAt) >= StaleAfter
}

func (r *Runner) launch(targets []Target) {
	if len(targets) == 0 {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.syncTargets(targets)
	}()
}

// syncTargets refreshes the NFL schedule and player news alongside league
// imports so provider-independent reads never delay league data. News uses the
// rosters saved by the previous sync, so roster changes appear on the next
// sync. The catalog loads once per batch, and one league's failure never stops
// the others.
func (r *Runner) syncTargets(targets []Target) {
	var batch sync.WaitGroup
	batch.Add(2)
	go func() {
		defer batch.Done()
		r.refreshSchedules(targets)
	}()
	go func() {
		defer batch.Done()
		r.RefreshPlayerNews(r.ctx)
	}()

	catalog, version := r.loadCatalog()
	for _, target := range targets {
		batch.Add(1)
		go func() {
			defer batch.Done()
			r.syncTarget(target, catalog, version)
		}()
	}
	batch.Wait()
}

func (r *Runner) loadCatalog() (playerids.Catalog, string) {
	if r.catalogPath == "" {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(r.ctx, time.Minute)
	defer cancel()
	catalog, version, err := playerids.Load(ctx, r.catalogPath)
	if err != nil {
		slog.Warn("player mapping unavailable; retaining provider identities")
		return nil, ""
	}
	return catalog, version
}

// syncTarget refreshes one league and records its outcome. The league stays
// marked as syncing until its outcome is visible to status reads.
func (r *Runner) syncTarget(target Target, catalog playerids.Catalog, version string) {
	defer r.release(target.ID)

	ctx, cancel := context.WithTimeout(r.ctx, leagueSyncTimeout)
	defer cancel()
	err := r.importTarget(ctx, target, catalog, version)
	if shouldMarkNeedsReconnect(err) {
		r.markNeedsReconnect(target)
	}
	r.recordOutcome(target, err)
}

func shouldMarkNeedsReconnect(err error) bool {
	if errors.Is(err, espn.ErrUnauthorized) {
		return true
	}
	var setup setupError
	return errors.As(err, &setup) && setup == "espn_credentials_unavailable"
}

func (r *Runner) markNeedsReconnect(target Target) {
	ctx, cancel := context.WithTimeout(context.Background(), targetReadTimeout)
	defer cancel()
	if err := r.db.Queries().MarkConnectionNeedsReconnect(ctx, target.ConnectionID); err != nil {
		slog.Warn("could not mark provider connection for reconnect", "league", target.ID)
	}
}

func (r *Runner) importTarget(ctx context.Context, target Target, catalog playerids.Catalog, version string) error {
	source, err := r.sources.SourceFor(target)
	if err != nil {
		return err
	}

	service := importer.Service{DB: r.db, Source: source, Catalog: catalog, CatalogVersion: version}
	_, err = service.Import(ctx, importer.Request{
		OwnerEmail:        r.ownerEmail,
		ExternalAccountID: target.ExternalAccountID,
		Selection:         target.Selection,
	})
	return err
}

// recordOutcome remembers a league's failure until its next successful sync.
func (r *Runner) recordOutcome(target Target, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err == nil {
		delete(r.failures, target.ID)
		return
	}

	code := failureCode(err)
	r.failures[target.ID] = failure{code: code, at: r.now()}
	slog.Warn("league sync failed", "league", target.ID, "provider", target.Selection.League.Provider, "code", code, "error", err)
}

func (r *Runner) release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, id)
}

// failureCode maps a sync error to a stable, credential-free code.
func failureCode(err error) string {
	var setup setupError
	var recorded *importer.RecordedSyncError
	switch {
	case errors.As(err, &setup):
		return string(setup)
	case errors.As(err, &recorded):
		return recorded.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "sync_timed_out"
	case errors.Is(err, context.Canceled):
		return "sync_canceled"
	default:
		return "provider_unavailable"
	}
}
