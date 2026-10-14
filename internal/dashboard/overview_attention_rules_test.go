package dashboard

import (
	"reflect"
	"testing"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAlertFingerprintTracksCurrentSituation(t *testing.T) {
	injury := &v1.LineupAlert{
		Id:           "team:player:injury",
		Kind:         v1.AlertKind_ALERT_KIND_INJURY,
		InjuryStatus: v1.InjuryStatus_INJURY_STATUS_QUESTIONABLE,
	}
	questionable := alertFingerprint(injury, 3)
	if got, want := questionable, "team:player:injury:week3:questionable"; got != want {
		t.Fatalf("questionable fingerprint = %q, want %q", got, want)
	}
	if got := alertFingerprint(injury, 3); got != questionable {
		t.Fatalf("same alert fingerprint = %q, want %q", got, questionable)
	}
	injury.InjuryStatus = v1.InjuryStatus_INJURY_STATUS_OUT
	if got := alertFingerprint(injury, 3); got == questionable {
		t.Fatalf("changed injury status kept fingerprint %q", got)
	}
	if got := alertFingerprint(injury, 4); got == alertFingerprint(injury, 3) {
		t.Fatalf("changed week kept fingerprint %q", got)
	}

	emptySlots := &v1.LineupAlert{
		Id:         "team:QB",
		Kind:       v1.AlertKind_ALERT_KIND_EMPTY_SLOT,
		EmptySlots: 1,
	}
	oneEmptySlot := alertFingerprint(emptySlots, 3)
	emptySlots.EmptySlots = 2
	if got := alertFingerprint(emptySlots, 3); got == oneEmptySlot {
		t.Fatalf("changed empty-slot count kept fingerprint %q", got)
	}

	bye := &v1.LineupAlert{Id: "team:player:bye", Kind: v1.AlertKind_ALERT_KIND_BYE}
	if got, want := alertFingerprint(bye, 3), "team:player:bye:week3"; got != want {
		t.Fatalf("bye fingerprint = %q, want %q", got, want)
	}
}

func TestWithoutDismissedPreservesUndismissedOrder(t *testing.T) {
	alerts := []*v1.LineupAlert{
		{Id: "first", Fingerprint: "first:week1"},
		{Id: "second", Fingerprint: "second:week1"},
		{Id: "third", Fingerprint: "third:week1"},
	}
	got := withoutDismissed(alerts, map[string]bool{"second:week1": true})
	want := []*v1.LineupAlert{alerts[0], alerts[2]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withoutDismissed = %v, want %v", got, want)
	}
}

func TestStarterAlerts(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	team := store.OverviewTeamsRow{Period: 6, Provider: "espn"}
	text := func(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
	entry := func(id byte, slot, status, proTeam string) store.ListSnapshotRosterForUserRow {
		return store.ListSnapshotRosterForUserRow{PlayerID: pgtype.UUID{Bytes: [16]byte{id}, Valid: true}, DisplayName: "Player " + slot, SlotLabel: text(slot), InjuryStatus: text(status), ProTeam: text(proTeam)}
	}
	game := func(team string, kickoff time.Time) store.ListNFLGamesForWeekRow {
		return store.ListNFLGamesForWeekRow{Team: team, KickoffAt: pgtype.Timestamptz{Time: kickoff, Valid: true}}
	}
	entries := []store.ListSnapshotRosterForUserRow{
		entry(1, "QB", "out", "KC"),
		entry(2, "WR", "questionable", "BUF"),
		entry(3, "RB", "doubtful", "DEN"), // game already started
		entry(4, "TE", "active", "DET"),   // no game this week
		entry(5, "BENCH", "out", "KC"),    // not a starter
		entry(6, "K", "unknown", ""),      // nothing known
		entry(7, "FLEX", "", "KC"),        // status unavailable
	}
	schedule := weekSchedule{games: map[string]store.ListNFLGamesForWeekRow{
		"KC":  game("KC", now.Add(time.Hour)),
		"BUF": game("BUF", now.Add(30*time.Minute)),
		"DEN": game("DEN", now.Add(-time.Hour)),
	}}

	alerts := starterAlerts(team, entries, schedule, now)
	sortAlerts(alerts)
	got := []string{}
	for _, alert := range alerts {
		got = append(got, alert.Severity.String()+":"+alert.Kind.String()+":"+alert.Slot)
	}
	want := []string{"ALERT_SEVERITY_ACTION:ALERT_KIND_INJURY:QB", "ALERT_SEVERITY_ACTION:ALERT_KIND_BYE:TE", "ALERT_SEVERITY_MONITOR:ALERT_KIND_INJURY:WR"}
	if len(got) != len(want) {
		t.Fatalf("alerts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("alerts = %v, want %v", got, want)
		}
	}
	if alerts[0].Title != "Your starting quarterback is out" || alerts[0].KickoffAt == nil {
		t.Fatalf("injury alert = %+v", alerts[0])
	}

	for _, alert := range starterAlerts(team, entries, schedule, now.Add(2*time.Hour)) {
		if alert.Kind == v1.AlertKind_ALERT_KIND_BYE {
			t.Fatalf("bye alert after the week's last kickoff: %+v", alert)
		}
	}

	withoutSchedule := starterAlerts(team, entries, weekSchedule{}, now)
	for _, alert := range withoutSchedule {
		if alert.Kind == v1.AlertKind_ALERT_KIND_BYE {
			t.Fatalf("bye inferred without a schedule: %+v", alert)
		}
	}
	if len(withoutSchedule) != 3 {
		t.Fatalf("alerts without schedule = %d, want 3 injury alerts", len(withoutSchedule))
	}
}
