package assistant

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestGetScheduleToolIsRegisteredBeforeActionTools(t *testing.T) {
	tools := (&Service{}).chatTools()
	tool := findAssistantToolByName(tools, "get_schedule")
	if tool == nil || tool.label(json.RawMessage(`{}`)) != "Checking the NFL schedule" || tool.propose != nil {
		t.Fatal("get_schedule is missing or mislabeled")
	}
	scheduleIndex, firstActionIndex := -1, -1
	for index, candidate := range tools {
		if candidate.definition.Name == "get_schedule" {
			scheduleIndex = index
		}
		if candidate.propose != nil && firstActionIndex < 0 {
			firstActionIndex = index
		}
	}
	if firstActionIndex >= 0 && scheduleIndex > firstActionIndex {
		t.Fatalf("get_schedule at %d registered after action tool at %d", scheduleIndex, firstActionIndex)
	}
}

func TestGetScheduleToolRejectsInvalidInputBeforeReading(t *testing.T) {
	tools := (&Service{}).chatTools()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no league or teams", `{}`, "pass league_season_id or teams"},
		{"too many teams", `{"teams":["KC","BUF","SF","DET","MIA","NYJ","NE","DAL","PHI"]}`, "pass at most 8 teams"},
		{"week below range", `{"teams":["KC"],"from_week":0}`, "weeks must be between 1 and 18"},
		{"week above range", `{"teams":["KC"],"to_week":19}`, "weeks must be between 1 and 18"},
		{"unknown field", `{"teams":["KC"],"week":3}`, "invalid tool input"},
		{"bad league id", `{"league_season_id":"league-1"}`, "invalid tool input"},
		{"blank team", `{"teams":[" "]}`, "invalid tool input"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runAssistantTool(context.Background(), tools, llm.ToolCall{
				ID: "schedule", Name: "get_schedule", Input: json.RawMessage(test.input),
			})
			if !result.IsError || result.Content != test.want {
				t.Fatalf("result = %+v, want error %q", result, test.want)
			}
		})
	}
}

func TestParseScheduleInputNormalizesTeams(t *testing.T) {
	request, err := parseScheduleInput(scheduleToolInput{Teams: []string{" kc", "KC", "buf", "wsh", "WAS"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(request.teams, []string{"KC", "BUF", "WAS"}) {
		t.Fatalf("teams = %v", request.teams)
	}
}

func TestResolveScheduleWeeks(t *testing.T) {
	week := func(value int) *int { return &value }
	tests := []struct {
		name     string
		request  scheduleRequest
		current  int32
		from, to int32
		err      string
	}{
		{name: "defaults to current week plus three", current: 6, from: 6, to: 9},
		{name: "default end stops at week 18", current: 17, from: 17, to: 18},
		{name: "explicit from", request: scheduleRequest{fromWeek: week(2)}, current: 6, from: 2, to: 5},
		{name: "explicit range", request: scheduleRequest{fromWeek: week(3), toWeek: week(8)}, from: 3, to: 8},
		{name: "span over six weeks", request: scheduleRequest{fromWeek: week(3), toWeek: week(9)}, err: "pass at most 6 weeks"},
		{name: "from after to", request: scheduleRequest{fromWeek: week(9), toWeek: week(8)}, err: "from_week must be at or before to_week"},
		{name: "to before default from", request: scheduleRequest{toWeek: week(4)}, current: 6, err: "from_week must be at or before to_week"},
		{name: "no current week", err: "pass from_week; no league has synced a current week yet"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			from, to, err := resolveScheduleWeeks(test.request, test.current)
			if test.err != "" {
				if err == nil || toolErrorMessage(err) != test.err {
					t.Fatalf("error = %v, want %q", err, test.err)
				}
				return
			}
			if err != nil || from != test.from || to != test.to {
				t.Fatalf("weeks = %d-%d, %v; want %d-%d", from, to, err, test.from, test.to)
			}
		})
	}
}

func TestBuildScheduleResultFromRealSchedule(t *testing.T) {
	games := scheduleRowsInRange(loadScheduleTestdata(t), 5, 8)
	byes := []store.AssistantScheduleTeamByesRow{
		{Team: "BUF", Known: true, ByeWeek: 7},
		{Team: "KC", Known: true, ByeWeek: 5},
		{Team: "FA", Known: false},
	}
	roster := []store.AssistantScheduleOwnerRosterRow{
		{PlayerName: "Josh Allen", Position: "QB", ProTeam: "BUF"},
		{PlayerName: "Unsigned RB", Position: "RB"},
	}

	result := buildScheduleResult(2026, 5, 8, byes, games, roster)
	content, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"season":2026,"from_week":5,"to_week":8,"teams":[` +
		`{"team":"BUF","bye_week":7,"games":[` +
		`{"week":5,"opponent":"LAR","kickoff":"2026-10-13T00:15:00Z"},` +
		`{"week":6,"opponent":"LV","kickoff":"2026-10-18T20:25:00Z"},` +
		`{"week":8,"opponent":"BAL","kickoff":"2026-11-01T18:00:00Z"}]},` +
		`{"team":"KC","bye_week":5,"games":[` +
		`{"week":6,"opponent":"LAC","kickoff":"2026-10-18T20:25:00Z"},` +
		`{"week":7,"opponent":"SEA","kickoff":"2026-10-26T00:20:00Z"},` +
		`{"week":8,"opponent":"DEN","kickoff":"2026-11-01T21:25:00Z"}]}],` +
		`"roster":[{"name":"Josh Allen","position":"QB","nfl_team":"BUF","bye_week":7,"on_bye_in_range":true},` +
		`{"name":"Unsigned RB","position":"RB","on_bye_in_range":false}]}`
	if string(content) != want {
		t.Fatalf("schedule result\n got %s\nwant %s", content, want)
	}
}

func TestBuildScheduleResultOmitsRosterWithoutLeague(t *testing.T) {
	result := buildScheduleResult(2026, 5, 8, nil, nil, nil)
	content, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"season":2026,"from_week":5,"to_week":8,"teams":[]}` {
		t.Fatalf("result = %s", content)
	}
}

// loadScheduleTestdata reads real 2026 ESPN schedule rows for BUF, DET, KC, and SF,
// saved from nfl_games after a sync.
func loadScheduleTestdata(t *testing.T) []store.AssistantScheduleGamesRow {
	t.Helper()
	raw, err := os.ReadFile("testdata/nfl_games_2026.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved []struct {
		Week      int32     `json:"week"`
		Team      string    `json:"team"`
		Opponent  string    `json:"opponent"`
		KickoffAt time.Time `json:"kickoff_at"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	rows := make([]store.AssistantScheduleGamesRow, 0, len(saved))
	for _, game := range saved {
		rows = append(rows, store.AssistantScheduleGamesRow{
			Team: game.Team, Week: game.Week, Opponent: game.Opponent,
			KickoffAt: pgtype.Timestamptz{Time: game.KickoffAt, Valid: true},
		})
	}
	return rows
}

func scheduleRowsInRange(rows []store.AssistantScheduleGamesRow, from, to int32) []store.AssistantScheduleGamesRow {
	var inRange []store.AssistantScheduleGamesRow
	for _, row := range rows {
		if row.Week >= from && row.Week <= to {
			inRange = append(inRange, row)
		}
	}
	return inRange
}
