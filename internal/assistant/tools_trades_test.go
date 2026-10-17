package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestEvaluateTradeToolIsWiredAndRejectsInvalidSides(t *testing.T) {
	tool := findAssistantToolByName(tradeTools(&Service{}), "evaluate_trade")
	if tool == nil || tool.label(json.RawMessage(`{}`)) != "Evaluating trade" ||
		!strings.Contains(string(tool.definition.InputSchema), `"maxItems": 4`) {
		t.Fatal("evaluate_trade definition is missing")
	}
	result := runAssistantTool(context.Background(), []assistantTool{*tool}, llm.ToolCall{
		ID: "invalid", Name: "evaluate_trade",
		Input: json.RawMessage(`{
			"league_season_id": "00000000-0000-0000-0000-000000000001",
			"give_player_ids": [],
			"get_player_ids": ["00000000-0000-0000-0000-000000000002"]
		}`),
	})
	if !result.IsError || result.Content != "invalid tool input" {
		t.Fatalf("invalid trade sides: %+v", result)
	}
}

func TestTradeValidationErrorsNameTheProblem(t *testing.T) {
	if got := toolErrorMessage(toolValidationError{message: "Target WR is not on your team to give"}); got != "Target WR is not on your team to give" {
		t.Fatalf("trade error message %q", got)
	}
}

func TestParseTradeInputRejectsDuplicateAndOversizedSides(t *testing.T) {
	const (
		league = "00000000-0000-0000-0000-000000000001"
		player = "00000000-0000-0000-0000-000000000002"
		other  = "00000000-0000-0000-0000-000000000003"
		third  = "00000000-0000-0000-0000-000000000004"
		fourth = "00000000-0000-0000-0000-000000000005"
		fifth  = "00000000-0000-0000-0000-000000000006"
		sixth  = "00000000-0000-0000-0000-000000000007"
	)
	tests := []struct {
		name string
		give []string
		get  []string
	}{
		{
			name: "cross-side duplicate",
			give: []string{player},
			get:  []string{player},
		},
		{
			name: "same-side duplicate",
			give: []string{player, player},
			get:  []string{other},
		},
		{
			name: "five players on one side",
			give: []string{player, other, third, fourth, fifth},
			get:  []string{sixth},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseTradeInput(evaluateTradeInput{
				LeagueSeasonID: league,
				GivePlayerIDs:  test.give,
				GetPlayerIDs:   test.get,
			})
			if !errors.Is(err, errInvalidToolInput) {
				t.Fatalf("parseTradeInput error = %v, want %v", err, errInvalidToolInput)
			}
		})
	}
}

func TestTradeLimitation(t *testing.T) {
	if got := tradeLimitation([]store.AssistantTradeLineupCoverageRow{
		{HasLineup: true},
		{HasLineup: false},
	}); got != tradeLineupLimitation {
		t.Fatalf("limitation = %q, want %q", got, tradeLineupLimitation)
	}
	if got := tradeLimitation([]store.AssistantTradeLineupCoverageRow{{HasLineup: true}}); got != "" {
		t.Fatalf("limitation = %q, want empty", got)
	}
}

func TestTradeOpponentRejectsPlayersFromDifferentTeams(t *testing.T) {
	_, err := tradeOpponent([]tradePlayer{
		{teamID: "team-one"},
		{teamID: "team-two"},
	})
	if err == nil || err.Error() != "get players must all come from the same other team" {
		t.Fatalf("tradeOpponent error = %v", err)
	}
}
