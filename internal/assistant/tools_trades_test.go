package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/llm"
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
	if got := toolErrorMessage(tradeValidationError{message: "Target WR is not on your team to give"}); got != "Target WR is not on your team to give" {
		t.Fatalf("trade error message %q", got)
	}
}
