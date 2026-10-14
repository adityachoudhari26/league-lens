package leaguesync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func testRunner() *Runner {
	return &Runner{now: func() time.Time { return testNow }, running: map[string]bool{}, failures: map[string]failure{}}
}

func targetIDs(targets []Target) []string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	return ids
}

func TestClaimTargetsSkipsFreshFailedAndRunningLeagues(t *testing.T) {
	runner := testRunner()
	targets := []Target{
		{ID: "fresh", LastSuccessfulSyncAt: testNow.Add(-time.Minute)},
		{ID: "stale", LastSuccessfulSyncAt: testNow.Add(-StaleAfter)},
		{ID: "never"},
		{ID: "recently-failed", LastSuccessfulSyncAt: testNow.Add(-time.Hour), LastRun: CompletedRun{Status: "failed", CompletedAt: testNow.Add(-time.Minute)}},
		{ID: "failed-long-ago", LastSuccessfulSyncAt: testNow.Add(-time.Hour), LastRun: CompletedRun{Status: "failed", CompletedAt: testNow.Add(-failureCooldown)}},
	}

	staleClaims := runner.claimTargets(targets, true)
	if got, want := targetIDs(staleClaims), []string{"stale", "never", "failed-long-ago"}; !slices.Equal(got, want) {
		t.Fatalf("stale claims = %v, want %v", got, want)
	}

	explicitClaims := runner.claimTargets(targets, false)
	if got, want := targetIDs(explicitClaims), []string{"fresh", "recently-failed"}; !slices.Equal(got, want) {
		t.Fatalf("explicit claims = %v, want %v (running leagues must not be claimed twice)", got, want)
	}
}

func TestStatusOfDerivesState(t *testing.T) {
	succeeded := testNow.Add(-time.Hour)
	for _, tc := range []struct {
		name          string
		target        Target
		running       bool
		memoryFailure *failure
		wantState     string
		wantCode      string
	}{
		{name: "never synced", target: Target{ID: "x"}, wantState: StateNeverSynced},
		{name: "succeeded", target: Target{ID: "x", LastSuccessfulSyncAt: succeeded}, wantState: StateSucceeded},
		{name: "syncing wins over failure", target: Target{ID: "x", LastRun: CompletedRun{Status: "failed", CompletedAt: testNow}}, running: true, wantState: StateSyncing},
		{name: "saved failure", target: Target{ID: "x", LastSuccessfulSyncAt: succeeded, LastRun: CompletedRun{Status: "failed", ErrorCode: "provider_fetch_failed", CompletedAt: testNow}}, wantState: StateFailed, wantCode: "provider_fetch_failed"},
		{name: "setup failure", target: Target{ID: "x", LastSuccessfulSyncAt: succeeded}, memoryFailure: &failure{code: "espn_credentials_missing", at: testNow}, wantState: StateFailed, wantCode: "espn_credentials_missing"},
		{name: "failure before last success", target: Target{ID: "x", LastSuccessfulSyncAt: succeeded}, memoryFailure: &failure{code: "sync_timed_out", at: succeeded.Add(-time.Minute)}, wantState: StateSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := testRunner()
			runner.running[tc.target.ID] = tc.running
			if tc.memoryFailure != nil {
				runner.failures[tc.target.ID] = *tc.memoryFailure
			}

			status := runner.statusOf(tc.target)
			if status.State != tc.wantState || status.ErrorCode != tc.wantCode {
				t.Fatalf("status = %s/%q, want %s/%q", status.State, status.ErrorCode, tc.wantState, tc.wantCode)
			}
		})
	}
}

func TestFailureCodeNeverExposesErrorText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{setupError("espn_account_mismatch"), "espn_account_mismatch"},
		{fmt.Errorf("fetch: %w", context.DeadlineExceeded), "sync_timed_out"},
		{errors.New("espn returned 401 for cookie=private"), "provider_unavailable"},
	} {
		if got := failureCode(tc.err); got != tc.want {
			t.Errorf("failureCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
