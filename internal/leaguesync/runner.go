package leaguesync

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/jackc/pgx/v5"
)

const (
	// StaleAfter is how old saved data may be before a load-triggered sync refreshes it.
	StaleAfter = 15 * time.Minute

	// failureCooldown stops load-triggered syncs from retrying a failing league on every page view.
	failureCooldown = 5 * time.Minute

	leagueSyncTimeout = 5 * time.Minute
	targetReadTimeout = 5 * time.Second
)

// Runner refreshes the local owner's leagues in the background through the
// shared importer. Each league has at most one sync in flight; in-flight state
// lives in memory, so it does not survive a restart.
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

// failure is a sync failure that happened before the importer started a sync run.
type failure struct {
	code string
	at   time.Time
}

// New creates a runner. An empty catalogPath skips the optional player mapping.
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
// With onlyStale, leagues with fresh data or a recent failure are skipped.
func (r *Runner) Start(ctx context.Context, onlyStale bool) ([]Status, error) {
	targets, err := r.listTargets(ctx)
	if err != nil {
		return nil, err
	}

	claimedTargets := r.claimTargets(targets, onlyStale)
	r.launch(claimedTargets)
	return r.statuses(targets), nil
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
	ctx, cancel := context.WithTimeout(ctx, targetReadTimeout)
	defer cancel()

	queries := r.db.Queries()
	owner, err := queries.GetUserByEmail(ctx, r.ownerEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := queries.ListSyncTargets(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, targetFromRow(row))
	}
	return targets, nil
}

// claimTargets marks the targets this request will sync, skipping leagues
// that are already syncing so concurrent requests never duplicate work.
func (r *Runner) claimTargets(targets []Target, onlyStale bool) []Target {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	var claimed []Target
	for _, target := range targets {
		if r.running[target.ID] || (onlyStale && !r.dueForRefresh(target, now)) {
			continue
		}
		r.running[target.ID] = true
		claimed = append(claimed, target)
	}
	return claimed
}

// dueForRefresh reports whether saved data is stale, unless the league failed
// within the cooldown. Callers must hold r.mu.
func (r *Runner) dueForRefresh(target Target, now time.Time) bool {
	lastFailure := r.latestFailure(target)
	if !lastFailure.at.IsZero() && now.Sub(lastFailure.at) < failureCooldown {
		return false
	}
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

// syncTargets loads the optional player catalog once per batch, then refreshes
// leagues concurrently; one league's failure never stops the others.
func (r *Runner) syncTargets(targets []Target) {
	catalog, version := r.loadCatalog()

	var batch sync.WaitGroup
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
	r.recordOutcome(target, err)
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

// recordOutcome keeps failures the importer could not save on a sync run.
// Saved failures and successes are read back from Postgres instead.
func (r *Runner) recordOutcome(target Target, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var recorded *importer.RecordedSyncError
	switch {
	case err == nil:
		delete(r.failures, target.ID)
		return
	case errors.As(err, &recorded):
		delete(r.failures, target.ID)
		slog.Warn("league sync failed", "league", target.ID, "provider", target.Selection.League.Provider, "code", recorded.Code)
	default:
		code := failureCode(err)
		r.failures[target.ID] = failure{code: code, at: r.now()}
		slog.Warn("league sync failed", "league", target.ID, "provider", target.Selection.League.Provider, "code", code)
	}
}

func (r *Runner) release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, id)
}

func failureCode(err error) string {
	var setup setupError
	switch {
	case errors.As(err, &setup):
		return string(setup)
	case errors.Is(err, context.DeadlineExceeded):
		return "sync_timed_out"
	case errors.Is(err, context.Canceled):
		return "sync_canceled"
	default:
		return "provider_unavailable"
	}
}
