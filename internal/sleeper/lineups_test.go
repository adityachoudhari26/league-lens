package sleeper

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

// lineupFixtureData loads two weeks: week 1 is history (player 40 was dropped
// since) and week 2 is the observed week, where player 20 is on reserve. Only
// week 1 has projections, as if week 2's request failed.
func lineupFixtureData(t *testing.T) fantasy.LeagueData {
	t.Helper()
	raw := Data{Period: 2, ObservedAt: time.Now().UTC(), State: State{SeasonType: "regular"}}
	for name, target := range map[string]any{"league.json": &raw.League, "rosters.json": &raw.Rosters, "lineup_weeks.json": &raw.Weeks, "lineup_projections.json": &raw.Projections} {
		body, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(body, target); err != nil {
			t.Fatal(err)
		}
	}
	data, err := normalizeData(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func findLineup(t *testing.T, data fantasy.LeagueData, period fantasy.MatchupPeriod, teamID string) map[string]fantasy.LineupEntry {
	t.Helper()
	for _, lineup := range data.Lineups {
		if lineup.Period == period && lineup.TeamID == teamID {
			entries := map[string]fantasy.LineupEntry{}
			for _, entry := range lineup.Entries {
				entries[entry.PlayerID] = entry
			}
			return entries
		}
	}
	t.Fatalf("no week %d lineup for team %s", period, teamID)
	return nil
}

func TestLineupEntriesCarrySlotsStartersAndPoints(t *testing.T) {
	data := lineupFixtureData(t)
	if len(data.Lineups) != 4 {
		t.Fatalf("lineups = %d, want 4", len(data.Lineups))
	}

	history := findLineup(t, data, 1, "1")
	want := map[string]fantasy.LineupEntry{
		"10": {PlayerID: "10", ProviderSlotID: "0", Slot: fantasy.SlotQB, Starter: true},
		"40": {PlayerID: "40", ProviderSlotID: "1", Slot: fantasy.SlotFlex, Starter: true},
		"20": {PlayerID: "20", Slot: fantasy.SlotBench},
	}
	wantPoints := map[string]float64{"10": 12.5, "40": 7.25, "20": 3}
	for id, expected := range want {
		got := history[id]
		if got.ProviderSlotID != expected.ProviderSlotID || got.Slot != expected.Slot || got.Starter != expected.Starter {
			t.Fatalf("week 1 player %s = %+v, want %+v", id, got, expected)
		}
		if got.Points == nil || *got.Points != wantPoints[id] {
			t.Fatalf("week 1 player %s points = %v, want %v", id, got.Points, wantPoints[id])
		}
	}

	emptySlot := findLineup(t, data, 1, "2")
	if _, placeholder := emptySlot["0"]; placeholder || len(emptySlot) != 2 {
		t.Fatalf("empty starter slot became a player: %+v", emptySlot)
	}
	if defense := emptySlot["DET"]; defense.Starter || defense.Points != nil {
		t.Fatalf("unreported bench points = %+v, want nil points", defense)
	}

	current := findLineup(t, data, 2, "1")
	if reserve := current["20"]; reserve.Slot != fantasy.SlotIR || reserve.Starter {
		t.Fatalf("observed-week reserve = %+v, want non-starting IR", reserve)
	}
	if zero := current["10"]; zero.Points == nil || *zero.Points != 0 {
		t.Fatalf("zero points = %v, want reported zero", zero.Points)
	}
}

// The fixture league scores only receptions (1 point), so projections must
// come from the stat lines rather than the generic pts_* totals.
func TestLineupProjectionsUseLeagueScoring(t *testing.T) {
	data := lineupFixtureData(t)
	history := findLineup(t, data, 1, "1")
	for id, want := range map[string]float64{"10": 4.5, "40": 6, "20": 0} {
		got := history[id].ProjectedPoints
		if got == nil || *got != want {
			t.Fatalf("week 1 player %s projection = %v, want %v", id, got, want)
		}
	}
	if points := history["10"].Points; points == nil || *points != 12.5 {
		t.Fatalf("projection changed actual points: %v", points)
	}
	if defense := findLineup(t, data, 1, "2")["DET"]; defense.ProjectedPoints != nil {
		t.Fatalf("unprojected player = %v, want nil", *defense.ProjectedPoints)
	}
	for id, entry := range findLineup(t, data, 2, "1") {
		if entry.ProjectedPoints != nil {
			t.Fatalf("week 2 player %s projection = %v without week 2 projections", id, *entry.ProjectedPoints)
		}
	}
}

func TestLineupStartersSumToTeamScore(t *testing.T) {
	data := lineupFixtureData(t)
	scores := map[fantasy.MatchupPeriod]map[string]float64{}
	for _, matchup := range data.Matchups {
		scores[matchup.Period] = map[string]float64{matchup.TeamA.TeamID: *matchup.TeamA.Score, matchup.TeamB.TeamID: *matchup.TeamB.Score}
	}
	for _, lineup := range data.Lineups {
		total := 0.0
		for _, entry := range lineup.Entries {
			if entry.Starter {
				total += *entry.Points
			}
		}
		if math.Abs(total-scores[lineup.Period][lineup.TeamID]) > 1e-9 {
			t.Fatalf("week %d team %s starters = %v, score %v", lineup.Period, lineup.TeamID, total, scores[lineup.Period][lineup.TeamID])
		}
	}
}

func TestLineupReferencesExist(t *testing.T) {
	data := lineupFixtureData(t)
	players := map[string]bool{}
	for _, player := range data.Players {
		players[player.ExternalID] = true
	}
	for _, lineup := range data.Lineups {
		referenced := false
		for _, matchup := range data.Matchups {
			if matchup.Period == lineup.Period && matchup.ExternalID == lineup.MatchupID {
				for _, teamID := range matchupTeamIDs(matchup) {
					referenced = referenced || teamID == lineup.TeamID
				}
			}
		}
		if !referenced {
			t.Fatalf("lineup %+v references no matchup side", lineup)
		}
		for _, entry := range lineup.Entries {
			if !players[entry.PlayerID] {
				t.Fatalf("lineup player %s missing from players", entry.PlayerID)
			}
		}
	}
	if !players["40"] {
		t.Fatal("dropped player 40 missing from players")
	}
}

func TestLineupsSkipUnavailableRows(t *testing.T) {
	rows := []Matchup{{RosterID: 1}, {RosterID: 2, Players: []string{"30"}, Starters: []string{"30"}}}
	matchups, err := normalizeMatchups(rows, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	lineups, err := normalizeLineups(rows, matchups, []string{"QB"}, nil, nil, nil)
	if err != nil || len(lineups) != 1 || lineups[0].TeamID != "2" {
		t.Fatalf("lineups = %+v, %v; want only team 2", lineups, err)
	}
}
