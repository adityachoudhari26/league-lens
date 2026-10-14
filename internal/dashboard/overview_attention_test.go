package dashboard

import (
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestEmptyStarterSlots(t *testing.T) {
	settings := fantasy.LeagueSettings{RosterSlots: []fantasy.SlotDefinition{{Slot: fantasy.SlotRB, Count: 2}, {Slot: fantasy.SlotFlex, Count: 1}, {Slot: fantasy.SlotFlex, Count: 1}, {Slot: fantasy.SlotBench, Count: 6}}}
	entries := []store.ListSnapshotRosterForUserRow{{SlotLabel: pgtype.Text{String: "RB", Valid: true}}, {SlotLabel: pgtype.Text{String: "FLEX", Valid: true}}, {SlotLabel: pgtype.Text{String: "BENCH", Valid: true}}}
	gaps := emptyStarterSlots(settings, entries)
	if len(gaps) != 2 || gaps[0].slot != "FLEX" || gaps[0].count != 1 || gaps[1].slot != "RB" || gaps[1].count != 1 {
		t.Fatalf("incorrect gaps: %+v", gaps)
	}
	entries = append(entries, store.ListSnapshotRosterForUserRow{SlotLabel: pgtype.Text{String: "UNKNOWN", Valid: true}})
	if gaps := emptyStarterSlots(settings, entries); len(gaps) != 0 {
		t.Fatalf("unknown slots should prevent inference: %+v", gaps)
	}
}
