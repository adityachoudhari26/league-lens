package leaguesync

import "time"

const (
	StateSyncing     = "syncing"
	StateSucceeded   = "succeeded"
	StateFailed      = "failed"
	StateNeverSynced = "never_synced"
)

// Status is a league's sync state. A failed league still serves the data from
// its last successful sync.
type Status struct {
	Target    Target
	State     string
	FailedAt  time.Time
	ErrorCode string
}

func (r *Runner) statuses(targets []Target) []Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	statuses := make([]Status, 0, len(targets))
	for _, target := range targets {
		statuses = append(statuses, r.statusOf(target))
	}
	return statuses
}

// statusOf derives a league's state. Callers must hold r.mu.
func (r *Runner) statusOf(target Target) Status {
	status := Status{Target: target}
	lastFailure := r.latestFailure(target)
	switch {
	case r.running[target.ID]:
		status.State = StateSyncing
	case !lastFailure.at.IsZero():
		status.State = StateFailed
		status.FailedAt = lastFailure.at
		status.ErrorCode = lastFailure.code
	case !target.LastSuccessfulSyncAt.IsZero():
		status.State = StateSucceeded
	default:
		status.State = StateNeverSynced
	}
	return status
}

// latestFailure returns the newest failure since the last success, whether it
// happened before the import started (memory) or during it (saved sync run).
// Callers must hold r.mu.
func (r *Runner) latestFailure(target Target) failure {
	latest := r.failures[target.ID]
	if target.LastRun.Status == "failed" && target.LastRun.CompletedAt.After(latest.at) {
		latest = failure{code: target.LastRun.ErrorCode, at: target.LastRun.CompletedAt}
	}
	if !latest.at.After(target.LastSuccessfulSyncAt) {
		return failure{}
	}
	return latest
}
