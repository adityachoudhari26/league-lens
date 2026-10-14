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
	lastFailure, failed := r.failures[target.ID]
	switch {
	case r.running[target.ID]:
		status.State = StateSyncing
	case failed:
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
