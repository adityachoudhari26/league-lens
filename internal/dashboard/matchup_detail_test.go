package dashboard

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestLineupGameState(t *testing.T) {
	now := time.Date(2026, time.September, 13, 17, 0, 0, 0, time.UTC)
	future := weekSchedule{games: map[string]store.ListNFLGamesForWeekRow{
		"KC": {Team: "KC", KickoffAt: pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}},
	}}
	past := weekSchedule{games: map[string]store.ListNFLGamesForWeekRow{
		"KC": {Team: "KC", KickoffAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}},
	}}
	equal := weekSchedule{games: map[string]store.ListNFLGamesForWeekRow{
		"KC": {Team: "KC", KickoffAt: pgtype.Timestamptz{Time: now, Valid: true}},
	}}

	tests := []struct {
		name        string
		proTeam     string
		schedule    weekSchedule
		wantState   v1.GameState
		wantKickoff bool
	}{
		{name: "empty pro team", schedule: future, wantState: v1.GameState_GAME_STATE_UNKNOWN},
		{name: "schedule not loaded", proTeam: "KC", wantState: v1.GameState_GAME_STATE_UNKNOWN},
		{name: "bye", proTeam: "NYJ", schedule: future, wantState: v1.GameState_GAME_STATE_BYE},
		{name: "future kickoff", proTeam: "KC", schedule: future, wantState: v1.GameState_GAME_STATE_NOT_STARTED, wantKickoff: true},
		{name: "past kickoff", proTeam: "KC", schedule: past, wantState: v1.GameState_GAME_STATE_KICKED_OFF, wantKickoff: true},
		{name: "kickoff now", proTeam: "KC", schedule: equal, wantState: v1.GameState_GAME_STATE_KICKED_OFF, wantKickoff: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, kickoff := lineupGameState(test.proTeam, test.schedule, now)
			if state != test.wantState {
				t.Fatalf("state = %v, want %v", state, test.wantState)
			}
			if (kickoff != nil) != test.wantKickoff {
				t.Fatalf("kickoff = %v, want present %t", kickoff, test.wantKickoff)
			}
			if kickoff != nil && !kickoff.AsTime().Equal(test.schedule.games["KC"].KickoffAt.Time) {
				t.Fatalf("kickoff timestamp = %v, want %v", kickoff.AsTime(), test.schedule.games["KC"].KickoffAt.Time)
			}
		})
	}
}

func TestSingleScoringWeek(t *testing.T) {
	tests := []struct {
		name      string
		raw       []byte
		wantWeek  int32
		wantFound bool
		wantError bool
	}{
		{name: "one week", raw: []byte("[2]"), wantWeek: 2, wantFound: true},
		{name: "multiple weeks", raw: []byte("[1,2]")},
		{name: "empty", raw: []byte("[]")},
		{name: "missing", raw: nil},
		{name: "null", raw: []byte("null")},
		{name: "invalid JSON", raw: []byte("invalid"), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			week, found, err := singleScoringWeek(test.raw)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, want error %t", err, test.wantError)
			}
			if week != test.wantWeek || found != test.wantFound {
				t.Fatalf("singleScoringWeek = (%d, %t), want (%d, %t)", week, found, test.wantWeek, test.wantFound)
			}
		})
	}
}

func TestMultiWeekMatchupUsesUnknownGameState(t *testing.T) {
	_, hasSingleWeek, err := singleScoringWeek([]byte("[1,2]"))
	if err != nil || hasSingleWeek {
		t.Fatalf("multi-week scoring periods returned (%t, %v)", hasSingleWeek, err)
	}
	state, kickoff := lineupGameState("KC", weekSchedule{}, time.Now())
	if state != v1.GameState_GAME_STATE_UNKNOWN || kickoff != nil {
		t.Fatalf("multi-week game state = %v with kickoff %v, want unknown", state, kickoff)
	}
}

func TestSortLineupPlayersByStarterSlots(t *testing.T) {
	players := []*v1.LineupPlayer{
		playerInSlot("def", "DEF", "DEF", 0),
		playerInSlot("flex", "FLEX", "FLEX", 0),
		playerInSlot("superflex", "SUPERFLEX", "SUPERFLEX", 0),
		playerInSlot("te", "TE", "TE", 0),
		playerInSlot("rb", "RB", "RB", 0),
		playerInSlot("wr", "WR", "WR", 0),
		playerInSlot("k", "K", "K", 0),
		playerInSlot("qb", "QB", "QB", 0),
		playerInSlot("unknown", "UNKNOWN", "Unknown slot", 0),
		playerInSlot("empty", "", "Empty slot", 0),
	}
	sortLineupPlayers(players, starterSlotOrder)

	assertPlayerSlots(t, players[:8], []string{"QB", "RB", "WR", "TE", "FLEX", "SUPERFLEX", "DEF", "K"})
	for _, player := range players[8:] {
		if player.Slot != "" && player.Slot != string(fantasy.SlotUnknown) {
			t.Fatalf("unknown slot was not sorted last: %v", player)
		}
	}
}

func TestSortLineupPlayersByPointsNameAndID(t *testing.T) {
	players := []*v1.LineupPlayer{
		playerInSlotWithOptionalPoints("nil", "RB", "No points", nil),
		playerInSlot("low", "RB", "Low", 3),
		playerInSlot("high", "RB", "High", 8),
		playerInSlot("z", "RB", "Same", 4),
		playerInSlot("a", "RB", "Same", 4),
		playerInSlot("alpha", "RB", "Alpha", 4),
	}
	sortLineupPlayers(players, starterSlotOrder)
	assertPlayerIDs(t, players, []string{"high", "alpha", "a", "z", "low", "nil"})
}

func TestSortLineupPlayersByBenchSlots(t *testing.T) {
	players := []*v1.LineupPlayer{
		playerInSlot("ir", "IR", "IR", 0),
		playerInSlot("taxi", "TAXI", "Taxi", 0),
		playerInSlot("bench", "BENCH", "Bench", 0),
	}
	sortLineupPlayers(players, benchSlotOrder)
	assertPlayerSlots(t, players, []string{"BENCH", "TAXI", "IR"})
}

func TestStartersLeftToPlay(t *testing.T) {
	starters := []*v1.LineupPlayer{
		{GameState: v1.GameState_GAME_STATE_NOT_STARTED},
		{GameState: v1.GameState_GAME_STATE_UNKNOWN},
		{GameState: v1.GameState_GAME_STATE_BYE},
		{GameState: v1.GameState_GAME_STATE_KICKED_OFF},
	}
	if got := startersLeftToPlay(starters); got != 1 {
		t.Fatalf("startersLeftToPlay = %d, want 1", got)
	}
}

func TestProjectedScore(t *testing.T) {
	actual, projected, zero := 8.5, 14.25, 0.0
	tenth, fifth, threeTenths := 0.1, 0.2, 0.3
	notStarted, kickedOff := v1.GameState_GAME_STATE_NOT_STARTED, v1.GameState_GAME_STATE_KICKED_OFF
	tests := []struct {
		name     string
		starters []*v1.LineupPlayer
		want     *float64
	}{
		{"not started uses projection", []*v1.LineupPlayer{{GameState: notStarted, Points: &zero, ProjectedPoints: &projected}}, &projected},
		{"kicked off uses actual", []*v1.LineupPlayer{{GameState: kickedOff, Points: &actual, ProjectedPoints: &projected}}, &actual},
		{"bye and unknown use actual", []*v1.LineupPlayer{
			{GameState: v1.GameState_GAME_STATE_BYE, ProjectedPoints: &projected},
			{GameState: v1.GameState_GAME_STATE_UNKNOWN, Points: &actual, ProjectedPoints: &projected},
		}, &actual},
		{"missing projection counts as zero", []*v1.LineupPlayer{
			{GameState: notStarted},
			{GameState: notStarted, ProjectedPoints: &projected},
		}, &projected},
		{"float noise is rounded", []*v1.LineupPlayer{
			{GameState: kickedOff, Points: &tenth},
			{GameState: kickedOff, Points: &fifth, ProjectedPoints: &zero},
		}, &threeTenths},
		{"zero projection is present", []*v1.LineupPlayer{{GameState: notStarted, ProjectedPoints: &zero}}, &zero},
		{"no projections is absent", []*v1.LineupPlayer{{GameState: kickedOff, Points: &actual}}, nil},
		{"no starters is absent", nil, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := projectedScore(test.starters)
			if (got == nil) != (test.want == nil) || (got != nil && *got != *test.want) {
				t.Fatalf("projectedScore = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMatchupStatusMessage(t *testing.T) {
	tests := []struct {
		status string
		want   v1.MatchupStatus
	}{
		{status: "scheduled", want: v1.MatchupStatus_MATCHUP_STATUS_SCHEDULED},
		{status: "in_progress", want: v1.MatchupStatus_MATCHUP_STATUS_IN_PROGRESS},
		{status: "final", want: v1.MatchupStatus_MATCHUP_STATUS_FINAL},
		{status: "unknown", want: v1.MatchupStatus_MATCHUP_STATUS_UNKNOWN},
		{status: "unexpected", want: v1.MatchupStatus_MATCHUP_STATUS_UNKNOWN},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			if got := matchupStatusMessage(test.status); got != test.want {
				t.Fatalf("matchupStatusMessage(%q) = %v, want %v", test.status, got, test.want)
			}
		})
	}
}

func playerInSlot(id, slot, name string, points float64) *v1.LineupPlayer {
	return playerInSlotWithOptionalPoints(id, slot, name, &points)
}

func playerInSlotWithOptionalPoints(id, slot, name string, points *float64) *v1.LineupPlayer {
	return &v1.LineupPlayer{PlayerId: id, Slot: slot, PlayerName: name, Points: points}
}

func assertPlayerSlots(t *testing.T, players []*v1.LineupPlayer, want []string) {
	t.Helper()
	if len(players) != len(want) {
		t.Fatalf("player count = %d, want %d", len(players), len(want))
	}
	for index, player := range players {
		if player.Slot != want[index] {
			t.Fatalf("slot[%d] = %q, want %q", index, player.Slot, want[index])
		}
	}
}

func assertPlayerIDs(t *testing.T, players []*v1.LineupPlayer, want []string) {
	t.Helper()
	if len(players) != len(want) {
		t.Fatalf("player count = %d, want %d", len(players), len(want))
	}
	for index, player := range players {
		if player.PlayerId != want[index] {
			t.Fatalf("player[%d] = %q, want %q", index, player.PlayerId, want[index])
		}
	}
}
