//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestGetScheduleReadsStoredScheduleAndOwnerRoster(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	season := fantasy.CurrentSeason(now)
	importScheduleFixture(ctx, t, db, scheduleFixtureData(now, season))

	reader := dashboard.New(db, "schedule@example.test", nil, nil, nil)
	tools := (&Service{DB: db, OwnerEmail: "schedule@example.test", Dashboard: reader}).chatTools()
	leagues := runAssistantTool(ctx, tools, llm.ToolCall{ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`)})
	if leagues.IsError {
		t.Fatalf("list_leagues: %s", leagues.Content)
	}
	leagueID := findJSONField(t, leagues.Content, "id")
	callSchedule := func(input string) llm.ToolResult {
		return runAssistantTool(ctx, tools, llm.ToolCall{ID: "schedule", Name: "get_schedule", Input: json.RawMessage(input)})
	}

	notLoaded := callSchedule(`{"league_season_id":"` + leagueID + `"}`)
	if !notLoaded.IsError || notLoaded.Content != "the NFL schedule hasn't loaded yet; refresh leagues" {
		t.Fatalf("empty schedule result: %+v", notLoaded)
	}

	seedScheduleTestdata(ctx, t, db, season, now)
	withRoster := callSchedule(`{"league_season_id":"` + leagueID + `","teams":["sf"]}`)
	if withRoster.IsError {
		t.Fatalf("league schedule: %s", withRoster.Content)
	}
	var result scheduleToolResult
	if err := json.Unmarshal([]byte(withRoster.Content), &result); err != nil {
		t.Fatal(err)
	}
	assertScheduleWeeks(t, result, int32(season), 6, 9)
	assertScheduleTeams(t, result, map[string]scheduleTeamWant{
		"BUF": {bye: 7, weeks: []int32{6, 8, 9}},
		"KC":  {bye: 5, weeks: []int32{6, 7, 8, 9}},
		"SF":  {bye: 8, weeks: []int32{6, 7, 9}},
	})
	assertScheduleRoster(t, result.Roster, []scheduleRosterPlayer{
		{Name: "Bench RB", Position: "RB", OnByeInRange: false},
		{Name: "Bills QB", Position: "QB", NFLTeam: "BUF", ByeWeek: int32Pointer(7), OnByeInRange: true},
		{Name: "Chiefs WR", Position: "WR", NFLTeam: "KC", ByeWeek: int32Pointer(5), OnByeInRange: false},
	})

	teamsOnly := callSchedule(`{"teams":["DET"],"to_week":7}`)
	if teamsOnly.IsError {
		t.Fatalf("teams schedule: %s", teamsOnly.Content)
	}
	result = scheduleToolResult{}
	if err := json.Unmarshal([]byte(teamsOnly.Content), &result); err != nil {
		t.Fatal(err)
	}
	assertScheduleWeeks(t, result, int32(season), 6, 7)
	assertScheduleTeams(t, result, map[string]scheduleTeamWant{"DET": {bye: 6, weeks: []int32{7}}})
	if result.Roster != nil {
		t.Fatalf("teams-only result has a roster: %+v", result.Roster)
	}

	for input, want := range map[string]string{
		`{"teams":["XYZ"]}`:                            "unknown NFL team XYZ",
		`{"teams":["KC"],"from_week":1,"to_week":7}`:   "pass at most 6 weeks",
		`{"teams":["KC"],"from_week":9,"to_week":8}`:   "from_week must be at or before to_week",
		`{"league_season_id":"` + otherLeagueID + `"}`: "requested data not found",
	} {
		if rejected := callSchedule(input); !rejected.IsError || rejected.Content != want {
			t.Fatalf("%s: result %+v, want %q", input, rejected, want)
		}
	}
}

const otherLeagueID = "00000000-0000-0000-0000-000000000099"

type scheduleTeamWant struct {
	bye   int32
	weeks []int32
}

func assertScheduleWeeks(t *testing.T, result scheduleToolResult, season, from, to int32) {
	t.Helper()
	if result.Season != season || result.FromWeek != from || result.ToWeek != to {
		t.Fatalf("season/weeks = %d %d-%d, want %d %d-%d", result.Season, result.FromWeek, result.ToWeek, season, from, to)
	}
}

func assertScheduleTeams(t *testing.T, result scheduleToolResult, want map[string]scheduleTeamWant) {
	t.Helper()
	if len(result.Teams) != len(want) {
		t.Fatalf("teams = %+v, want %v", result.Teams, want)
	}
	for _, team := range result.Teams {
		expected, ok := want[team.Team]
		if !ok || team.ByeWeek == nil || *team.ByeWeek != expected.bye || len(team.Games) != len(expected.weeks) {
			t.Fatalf("team %+v, want %+v", team, expected)
		}
		for index, game := range team.Games {
			if game.Week != expected.weeks[index] || game.Opponent == "" || game.Kickoff.IsZero() {
				t.Fatalf("%s game %d = %+v, want week %d", team.Team, index, game, expected.weeks[index])
			}
		}
	}
}

func assertScheduleRoster(t *testing.T, roster *[]scheduleRosterPlayer, want []scheduleRosterPlayer) {
	t.Helper()
	got, _ := json.Marshal(roster)
	expected, _ := json.Marshal(want)
	if string(got) != string(expected) {
		t.Fatalf("roster\n got %s\nwant %s", got, expected)
	}
}

func int32Pointer(value int32) *int32 { return &value }

func importScheduleFixture(ctx context.Context, t *testing.T, db *database.DB, data fantasy.LeagueData) {
	t.Helper()
	importService := importer.Service{DB: db, Source: assistantLeagueSource{data: data}}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: "schedule@example.test", ExternalAccountID: "sleeper",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// scheduleFixtureData is a two-team league whose owner roster spans a bye in weeks
// 6–9 (BUF), a bye before it (KC), and a player without an NFL team.
func scheduleFixtureData(now time.Time, season int) fantasy.LeagueData {
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "schedule-fixture", Season: season}
	snapshot := func(team string, entries ...fantasy.RosterEntry) fantasy.RosterSnapshot {
		return fantasy.RosterSnapshot{
			TeamID: team, ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
			Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: entries,
		}
	}
	entry := func(player string, position fantasy.Position, slot fantasy.RosterSlot, team string) fantasy.RosterEntry {
		return fantasy.RosterEntry{
			PlayerID: player, Slot: slot, EligiblePositions: []fantasy.Position{position}, ProTeam: team,
		}
	}
	return fantasy.LeagueData{
		League:    fantasy.League{Ref: ref, Name: "Schedule Fixture", TeamCount: 2},
		FetchedAt: now,
		Teams:     []fantasy.Team{{ExternalID: "home", Name: "Home Team"}, {ExternalID: "away", Name: "Away Team"}},
		Players: []fantasy.Player{
			{ExternalID: "bills-qb", Name: "Bills QB", Kind: fantasy.PlayerPerson},
			{ExternalID: "chiefs-wr", Name: "Chiefs WR", Kind: fantasy.PlayerPerson},
			{ExternalID: "bench-rb", Name: "Bench RB", Kind: fantasy.PlayerPerson},
			{ExternalID: "lions-te", Name: "Lions TE", Kind: fantasy.PlayerPerson},
		},
		Rosters: []fantasy.RosterSnapshot{
			snapshot("home",
				entry("bills-qb", fantasy.PositionQB, fantasy.SlotQB, "BUF"),
				entry("chiefs-wr", fantasy.PositionWR, fantasy.SlotWR, "KC"),
				entry("bench-rb", fantasy.PositionRB, fantasy.SlotRB, ""),
			),
			snapshot("away", entry("lions-te", fantasy.PositionTE, fantasy.SlotTE, "DET")),
		},
	}
}

// seedScheduleTestdata stores the saved real schedule rows under the fixture season.
func seedScheduleTestdata(ctx context.Context, t *testing.T, db *database.DB, season int, now time.Time) {
	t.Helper()
	games := loadScheduleTestdata(t)
	err := db.InTx(ctx, func(q *store.Queries) error {
		for _, game := range games {
			if err := q.AddNFLGame(ctx, store.AddNFLGameParams{
				Season: int32(season), Week: game.Week, Team: game.Team, Opponent: game.Opponent,
				KickoffAt: game.KickoffAt, FetchedAt: pgtype.Timestamptz{Time: now, Valid: true},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
