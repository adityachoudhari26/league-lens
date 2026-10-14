package importer

import (
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"math"
	"strings"
	"testing"
	"time"
)

func TestRejectMultipleSelectedTeams(t *testing.T) {
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league", Season: 2026}
	league := fantasy.League{Ref: ref}
	data := fantasy.LeagueData{League: league}
	selection := fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{"1", "2"}}

	err := validateData(data, league, selection)
	if err == nil || !strings.Contains(err.Error(), "more than one selected team") {
		t.Fatalf("expected multiple selected teams to be rejected, got %v", err)
	}
}

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

// lineupPayload is a minimal valid league with one matchup and both sides'
// lineups, for validation cases to mutate.
func lineupPayload() (fantasy.LeagueData, fantasy.League, fantasy.ConnectionSelection) {
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league", Season: 2026}
	league := fantasy.League{Ref: ref, TeamCount: 2}
	ten := 10.0
	data := fantasy.LeagueData{
		League:    league,
		FetchedAt: time.Now(),
		Teams:     []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}},
		Players:   []fantasy.Player{{ExternalID: "10", Name: "QB", Kind: fantasy.PlayerPerson}, {ExternalID: "20", Name: "RB", Kind: fantasy.PlayerPerson}},
		Matchups:  []fantasy.Matchup{{ExternalID: "m1", Period: 1, TeamA: fantasy.MatchupSide{TeamID: "1"}, TeamB: &fantasy.MatchupSide{TeamID: "2"}, Status: fantasy.MatchupFinal}},
		Lineups: []fantasy.MatchupLineup{
			{Period: 1, MatchupID: "m1", TeamID: "1", Entries: []fantasy.LineupEntry{{PlayerID: "10", Slot: fantasy.SlotQB, Starter: true, Points: &ten}, {PlayerID: "20", Slot: fantasy.SlotBench}}},
			{Period: 1, MatchupID: "m1", TeamID: "2", Entries: []fantasy.LineupEntry{}},
		},
	}
	return data, league, fantasy.ConnectionSelection{League: ref}
}

func TestValidateMatchupLineups(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		name   string
		mutate func(*fantasy.LeagueData)
		want   string
	}{
		{"valid", func(*fantasy.LeagueData) {}, ""},
		{"unknown matchup", func(d *fantasy.LeagueData) { d.Lineups[0].MatchupID = "other" }, "invalid matchup lineup"},
		{"wrong period", func(d *fantasy.LeagueData) { d.Lineups[0].Period = 2 }, "invalid matchup lineup"},
		{"team not a side", func(d *fantasy.LeagueData) { d.Lineups[0].TeamID = "3" }, "invalid matchup lineup"},
		{"opponent lineup on a bye", func(d *fantasy.LeagueData) { d.Matchups[0].TeamB, d.Matchups[0].IsBye = nil, true }, "invalid matchup lineup"},
		{"duplicate side", func(d *fantasy.LeagueData) { d.Lineups[1].TeamID = "1" }, "duplicate matchup lineup"},
		{"unknown player", func(d *fantasy.LeagueData) { d.Lineups[0].Entries[0].PlayerID = "missing" }, "invalid matchup lineup entry"},
		{"duplicate player", func(d *fantasy.LeagueData) { d.Lineups[0].Entries[1].PlayerID = "10" }, "invalid matchup lineup entry"},
		{"empty slot", func(d *fantasy.LeagueData) { d.Lineups[0].Entries[1].Slot = "" }, "invalid matchup lineup entry"},
		{"non-finite points", func(d *fantasy.LeagueData) { d.Lineups[0].Entries[0].Points = &nan }, "invalid matchup lineup entry"},
		{"non-finite projected points", func(d *fantasy.LeagueData) { d.Lineups[0].Entries[0].ProjectedPoints = &nan }, "invalid matchup lineup entry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, league, selection := lineupPayload()
			tc.mutate(&data)
			assertValidationResult(t, validateData(data, league, selection), tc.want)
		})
	}
}

func assertValidationResult(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("expected valid payload, got %v", err)
		}
		return
	}
	if err == nil || err.Error() != want {
		t.Fatalf("expected %q, got %v", want, err)
	}
}
