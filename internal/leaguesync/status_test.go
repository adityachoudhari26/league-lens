package leaguesync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/importer"
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

func TestClaimTargetsSkipsFreshAndRunningLeagues(t *testing.T) {
	runner := testRunner()
	targets := []Target{
		{ID: "fresh", LastSuccessfulSyncAt: testNow.Add(-time.Minute)},
		{ID: "stale", LastSuccessfulSyncAt: testNow.Add(-StaleAfter)},
		{ID: "never"},
	}

	staleClaims := runner.claimTargets(targets, true)
	if got, want := targetIDs(staleClaims), []string{"stale", "never"}; !slices.Equal(got, want) {
		t.Fatalf("stale claims = %v, want %v", got, want)
	}

	explicitClaims := runner.claimTargets(targets, false)
	if got, want := targetIDs(explicitClaims), []string{"fresh"}; !slices.Equal(got, want) {
		t.Fatalf("explicit claims = %v, want %v (running leagues must not be claimed twice)", got, want)
	}
}

func TestStatusOfDerivesState(t *testing.T) {
	succeeded := testNow.Add(-time.Hour)
	for _, tc := range []struct {
		name      string
		target    Target
		running   bool
		failure   *failure
		wantState string
		wantCode  string
	}{
		{name: "never synced", target: Target{ID: "x"}, wantState: StateNeverSynced},
		{name: "succeeded", target: Target{ID: "x", LastSuccessfulSyncAt: succeeded}, wantState: StateSucceeded},
		{
			name:      "syncing wins over failure",
			target:    Target{ID: "x"},
			running:   true,
			failure:   &failure{code: "provider_fetch_failed", at: testNow},
			wantState: StateSyncing,
		},
		{
			name:      "failed",
			target:    Target{ID: "x", LastSuccessfulSyncAt: succeeded},
			failure:   &failure{code: "espn_credentials_missing", at: testNow},
			wantState: StateFailed,
			wantCode:  "espn_credentials_missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := testRunner()
			runner.running[tc.target.ID] = tc.running
			if tc.failure != nil {
				runner.failures[tc.target.ID] = *tc.failure
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
		{fmt.Errorf("import: %w", &importer.RecordedSyncError{Code: "provider_fetch_failed", Err: errors.New("espn 500")}), "provider_fetch_failed"},
		{fmt.Errorf("fetch: %w", context.DeadlineExceeded), "sync_timed_out"},
		{errors.New("espn returned 401 for cookie=private"), "provider_unavailable"},
	} {
		if got := FailureCode(tc.err); got != tc.want {
			t.Errorf("FailureCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
