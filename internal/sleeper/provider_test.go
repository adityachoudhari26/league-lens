package sleeper

import (
	"encoding/json"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"os"
	"testing"
	"time"
)

var _ fantasy.Provider = (*Client)(nil)

func TestNormalizeLeagueData(t *testing.T) {
	data := Data{Period: 2, ObservedAt: time.Now().UTC(), State: State{SeasonType: "regular"}, Weeks: map[int][]Matchup{}}
	for name, target := range map[string]any{"league.json": &data.League, "rosters.json": &data.Rosters} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("testdata/week.json")
	if err != nil {
		t.Fatal(err)
	}
	var week []Matchup
	if err = json.Unmarshal(raw, &week); err != nil {
		t.Fatal(err)
	}
	data.Weeks[1] = week
	normalized, err := normalizeData(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Teams) != 2 || len(normalized.Rosters) != 4 || len(normalized.Players) != 4 {
		t.Fatal("lost league data")
	}
	if normalized.Rosters[0].Entries[1].Slot != fantasy.SlotIR {
		t.Fatal("reserve not normalized")
	}
	if normalized.Matchups[0].TeamA.Score == nil || *normalized.Matchups[0].TeamA.Score != 0 {
		t.Fatal("zero override lost")
	}
	if normalized.Matchups[0].Period != 1 || normalized.Matchups[0].ScoringPeriods[0] != 1 {
		t.Fatal("period lost")
	}
	if len(normalized.CompleteMatchupPeriods) != 1 {
		t.Fatal("coverage missing")
	}
	// Unavailable history must not authorize deleting prior evidence.
	data.Weeks[1] = nil
	normalized, err = normalizeData(data)
	if err != nil || len(normalized.CompleteMatchupPeriods) != 0 {
		t.Fatal("missing history became complete")
	}
}
