package importer

import (
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"testing"
)

func TestMissingScore(t *testing.T) {
	n, err := score(nil)
	if err != nil || n.Valid {
		t.Fatal("missing score became zero")
	}
}

func TestSnapshotHashIgnoresEntryOrder(t *testing.T) {
	snapshot := fantasy.RosterSnapshot{Coverage: fantasy.CoverageFull, Entries: []fantasy.RosterEntry{{PlayerID: "2", Slot: fantasy.SlotBench}, {PlayerID: "1", Slot: fantasy.SlotQB}}}
	first, err := snapshotHash(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Entries[0], snapshot.Entries[1] = snapshot.Entries[1], snapshot.Entries[0]
	second, err := snapshotHash(snapshot)
	if err != nil || first != second {
		t.Fatal("provider ordering changed identity")
	}
}
func TestRejectConflictingAlias(t *testing.T) {
	service := Service{Catalog: playerids.Catalog{"10": {IDs: map[string]string{"sleeper": "10", "espn": "100"}}}}
	_, err := service.playerIdentity(fantasy.ProviderSleeper, fantasy.Player{ExternalID: "10", Kind: fantasy.PlayerPerson, KnownIDs: []fantasy.PlayerID{{Namespace: fantasy.PlayerIDESPN, ExternalID: "other"}}})
	if err == nil {
		t.Fatal("conflicting aliases accepted")
	}
}
