package leaguesync

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

// ImportJob is a league import already begun by the connect request (league
// attached, sync run open). StartImports finishes it in the background.
type ImportJob struct {
	// Target is the league row the import attached, rebuilt the same way the
	// Runner builds sync targets: ID is Started.LeagueSeasonID().String()
	// (the same string TargetFromRow produces), and Selection carries the
	// league ref and teams the request picked.
	Target  Target
	Source  fantasy.Provider
	Started importer.Started
}

// StartImports marks each job's league as syncing before returning, then
// finishes the jobs in parallel in one background goroutine. A job whose
// league is already syncing is abandoned: its sync run is failed as
// sync_already_running and the running sync keeps ownership of its status.
func (r *Runner) StartImports(jobs []ImportJob) {
	claimed, duplicates := r.claimImports(jobs)
	for _, job := range duplicates {
		if err := (&importer.Service{DB: r.db}).Abandon(job.Started, "sync_already_running"); err != nil {
			slog.Warn("duplicate import's sync run could not be failed", "league", job.Target.ID, "error", err)
		}
	}
	if len(claimed) == 0 {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.finishImports(claimed)
	}()
}

// claimImports marks the jobs this call will finish, skipping leagues that
// already have a sync or import in flight so the work is never duplicated.
func (r *Runner) claimImports(jobs []ImportJob) (claimed, duplicates []ImportJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range jobs {
		if r.running[job.Target.ID] {
			duplicates = append(duplicates, job)
			continue
		}
		r.running[job.Target.ID] = true
		claimed = append(claimed, job)
	}
	return claimed, duplicates
}

// finishImports completes each claimed job concurrently, then refreshes the
// schedule and player news once any import succeeded. The refresh runs after
// the imports because news players come from the latest saved rosters.
func (r *Runner) finishImports(jobs []ImportJob) {
	catalog, version := r.loadCatalog()
	var succeeded atomic.Bool
	var batch sync.WaitGroup
	for _, job := range jobs {
		batch.Add(1)
		go func() {
			defer batch.Done()
			if err := r.finishImport(job, catalog, version); err == nil {
				succeeded.Store(true)
			}
		}()
	}
	batch.Wait()
	if succeeded.Load() {
		r.refreshScheduleAndNews()
	}
}

// finishImport completes one begun import and records its outcome. The league
// stays marked as syncing until its outcome is visible to status reads.
func (r *Runner) finishImport(job ImportJob, catalog playerids.Catalog, version string) error {
	defer r.release(job.Target.ID)
	ctx, cancel := context.WithTimeout(r.ctx, leagueSyncTimeout)
	defer cancel()
	service := importer.Service{DB: r.db, Source: job.Source, Catalog: catalog, CatalogVersion: version}
	_, err := service.Finish(ctx, job.Started)
	if ShouldMarkNeedsReconnect(err) {
		r.MarkNeedsReconnect(job.Target)
	}
	r.recordOutcome(job.Target, err)
	return err
}
