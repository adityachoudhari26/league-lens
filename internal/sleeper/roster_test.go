package sleeper

import "testing"

func TestRosterCoverageAndSlots(t *testing.T) {
	entries, coverage, err := rosterEntries(nil, []string{"10", "0"}, nil, nil, []string{"QB", "FLEX", "BN"})
	if err != nil || coverage != "lineup" || len(entries) != 1 || entries[0].Label != "QB" {
		t.Fatalf("lineup fallback: %v %s %v", entries, coverage, err)
	}
	entries, coverage, err = rosterEntries([]string{"10", "20", "DET"}, []string{"10", "0"}, []string{"20"}, nil, []string{"QB", "FLEX", "BN"})
	if err != nil || coverage != "full" || entries[1].Label != "IR" || entries[2].Label != "BN" {
		t.Fatalf("slots: %+v %v", entries, err)
	}
	for _, players := range [][]string{{"20"}, {"10", "10"}, {"0"}} {
		if _, _, err = rosterEntries(players, []string{"10"}, nil, nil, []string{"QB"}); err == nil {
			t.Fatal("accepted invalid roster")
		}
	}
	if _, _, err = rosterEntries([]string{"10"}, []string{"10", "10"}, nil, nil, []string{"QB", "FLEX"}); err == nil {
		t.Fatal("accepted duplicate starter")
	}
}
