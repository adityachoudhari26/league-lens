package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const (
	jaxGameJSON = `{"away":"HOU","home":"JAX","kickoff":"2026-10-18T13:30:00Z","status":"scheduled",` +
		`"line":"JAX -2.5","over_under":42.5,"line_source":"Draft Kings",` +
		`"weather":{"condition":"Rain","temperature_f":58}}`
	atlGameJSON = `{"away":"CHI","home":"ATL","kickoff":"2026-10-18T17:00:00Z","status":"in_progress",` +
		`"line":"CHI -1.5","over_under":51.5,"line_source":"Draft Kings"}`
	sfGameJSON  = `{"away":"WAS","home":"SF","kickoff":"2026-10-20T00:15:00Z","status":"scheduled"}`
	denGameJSON = `{"away":"SEA","home":"DEN","kickoff":"2026-10-16T00:15:00Z","status":"final"}`
)

type stubGameContextSource struct {
	result fantasy.NFLWeekGameContext
	err    error
	weeks  []int
}

func (source *stubGameContextSource) NFLGameContext(_ context.Context, week int) (fantasy.NFLWeekGameContext, error) {
	source.weeks = append(source.weeks, week)
	return source.result, source.err
}

func TestGameContextToolFiltersAndSerializesGames(t *testing.T) {
	source := testGameContextSource()
	service := &Service{GameContext: source}
	tools := service.chatTools()
	filtered := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "filtered", Name: "get_game_context", Input: json.RawMessage(`{"week":6,"teams":["jax","Atl"]}`),
	})
	wantFiltered := `{"season":2026,"week":6,"games":[` + jaxGameJSON + "," + atlGameJSON + `]}`
	if filtered.IsError || filtered.Content != wantFiltered {
		t.Fatalf("filtered result = %+v, want %s", filtered, wantFiltered)
	}

	all := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "all", Name: "get_game_context", Input: json.RawMessage(`{}`),
	})
	wantAll := `{"season":2026,"week":6,"games":[` +
		jaxGameJSON + "," + atlGameJSON + "," + sfGameJSON + "," + denGameJSON + `]}`
	if all.IsError || all.Content != wantAll {
		t.Fatalf("all-games result = %+v, want %s", all, wantAll)
	}
	if len(source.weeks) != 2 || source.weeks[0] != 6 || source.weeks[1] != 0 {
		t.Fatalf("source weeks = %v, want [6 0]", source.weeks)
	}
}

func TestGameContextToolRejectsInvalidInputsWithoutCallingSource(t *testing.T) {
	source := testGameContextSource()
	tools := (&Service{GameContext: source}).chatTools()
	inputs := []string{
		`{"week":0}`,
		`{"week":19}`,
		`{"teams":["KC","BUF","MIA","NE","NYJ","BAL","CIN","PIT","CLE"]}`,
		`{"teams":[]}`,
		`{"teams":["J1"]}`,
		`{"season":2026}`,
	}
	for _, input := range inputs {
		result := runAssistantTool(context.Background(), tools, llm.ToolCall{
			ID: "invalid", Name: "get_game_context", Input: json.RawMessage(input),
		})
		if !result.IsError || result.Content != "invalid tool input" {
			t.Errorf("input %s result = %+v, want invalid tool input", input, result)
		}
	}
	if len(source.weeks) != 0 {
		t.Fatalf("invalid inputs called source with weeks %v", source.weeks)
	}
}

func TestGameContextToolHidesSourceErrors(t *testing.T) {
	source := testGameContextSource()
	source.err = errors.New("ESPN said <html>secret body</html>")
	result := runAssistantTool(context.Background(), (&Service{GameContext: source}).chatTools(), llm.ToolCall{
		ID: "error", Name: "get_game_context", Input: json.RawMessage(`{}`),
	})
	if !result.IsError || result.Content != "couldn't load the requested data" {
		t.Fatalf("source error result = %+v", result)
	}
}

func TestGameContextToolRegistrationAndLabel(t *testing.T) {
	if findAssistantToolByName((&Service{}).chatTools(), "get_game_context") != nil {
		t.Fatal("game context tool registered without a source")
	}

	tools := (&Service{GameContext: testGameContextSource()}).chatTools()
	gameContextIndex, actionIndex := -1, -1
	for index, tool := range tools {
		if tool.definition.Name == "get_game_context" {
			gameContextIndex = index
		}
		if tool.definition.Name == "sync_leagues" {
			actionIndex = index
		}
	}
	if gameContextIndex < 0 || actionIndex < 0 || gameContextIndex >= actionIndex {
		t.Fatalf("game context/action tool indexes = %d/%d", gameContextIndex, actionIndex)
	}
	if label := toolLabel(tools, "get_game_context", json.RawMessage(`{}`)); label != "Checking game lines and weather" {
		t.Fatalf("game context label = %q", label)
	}
}

func TestGameStatusLabel(t *testing.T) {
	tests := []struct {
		name  string
		state fantasy.NFLGameState
		want  string
	}{
		{name: "scheduled", state: fantasy.NFLGameScheduled, want: "scheduled"},
		{name: "live", state: fantasy.NFLGameLive, want: "in_progress"},
		{name: "final", state: fantasy.NFLGameFinal, want: "final"},
		{name: "postponed", state: fantasy.NFLGamePostponed, want: "postponed"},
		{name: "unknown", state: fantasy.NFLGameState("unknown"), want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gameStatusLabel(test.state); got != test.want {
				t.Errorf("gameStatusLabel(%q) = %q, want %q", test.state, got, test.want)
			}
		})
	}
}

func testGameContextSource() *stubGameContextSource {
	jaxOverUnder := 42.5
	atlOverUnder := 51.5
	jaxTemperature := 58
	return &stubGameContextSource{result: fantasy.NFLWeekGameContext{
		Season: 2026,
		Week:   6,
		Games: []fantasy.NFLGameContext{
			{
				HomeTeam: "JAX", AwayTeam: "HOU",
				KickoffAt: time.Date(2026, time.October, 18, 13, 30, 0, 0, time.UTC),
				State:     fantasy.NFLGameScheduled,
				Odds: &fantasy.NFLGameOdds{
					Line: "JAX -2.5", OverUnder: &jaxOverUnder, Source: "Draft Kings",
				},
				Weather: &fantasy.NFLGameWeather{Condition: "Rain", TemperatureF: &jaxTemperature},
			},
			{
				HomeTeam: "ATL", AwayTeam: "CHI",
				KickoffAt: time.Date(2026, time.October, 18, 17, 0, 0, 0, time.UTC),
				State:     fantasy.NFLGameLive,
				Odds:      &fantasy.NFLGameOdds{Line: "CHI -1.5", OverUnder: &atlOverUnder, Source: "Draft Kings"},
			},
			{
				HomeTeam: "SF", AwayTeam: "WAS",
				KickoffAt: time.Date(2026, time.October, 20, 0, 15, 0, 0, time.UTC),
				State:     fantasy.NFLGameScheduled,
			},
			{
				HomeTeam: "DEN", AwayTeam: "SEA",
				KickoffAt: time.Date(2026, time.October, 16, 0, 15, 0, 0, time.UTC),
				State:     fantasy.NFLGameFinal,
			},
		},
	}}
}
