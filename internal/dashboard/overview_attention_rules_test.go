package dashboard

import (
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

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
		got = append(got, alert.Severity+":"+alert.Kind+":"+alert.Slot)
	}
	want := []string{"action:injury:QB", "action:bye:TE", "monitor:injury:WR"}
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

	withoutSchedule := starterAlerts(team, entries, weekSchedule{}, now)
	for _, alert := range withoutSchedule {
		if alert.Kind == "bye" {
			t.Fatalf("bye inferred without a schedule: %+v", alert)
		}
	}
	if len(withoutSchedule) != 3 {
		t.Fatalf("alerts without schedule = %d, want 3 injury alerts", len(withoutSchedule))
	}
}
