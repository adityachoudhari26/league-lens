package assistant

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

var (
	_ func(*Service, context.Context) (string, error)                = (*Service).createConversation
	_ func(*Service, context.Context, string) ([]llm.Message, error) = (*Service).loadConversation
	_ func(*Service, context.Context, string, []llm.Message) error   = (*Service).saveConversationTurn
)

func TestConversationRowsWithinBudgetKeepsAllTurnsThatFit(t *testing.T) {
	rows := []store.ListRecentAssistantMessagesRow{
		conversationBudgetRow(0, "old", "[]", "[]"),
		conversationBudgetRow(0, "answer", "[]", "[]"),
		conversationBudgetRow(1, "new", "[]", "[]"),
	}

	got := conversationRowsWithinBudget(rows, 24)
	if len(got) != len(rows) {
		t.Fatalf("kept %d rows, want all %d", len(got), len(rows))
	}
}

func TestConversationRowsWithinBudgetDropsOlderWholeTurns(t *testing.T) {
	rows := []store.ListRecentAssistantMessagesRow{
		conversationBudgetRow(0, "old", "[]", "[]"),
		conversationBudgetRow(0, "older", "[]", "[]"),
		conversationBudgetRow(1, "middle", "[]", "[]"),
		conversationBudgetRow(1, "reply", "[]", "[]"),
		conversationBudgetRow(2, "new", "[]", "[]"),
		conversationBudgetRow(2, "answer", "[]", "[]"),
	}

	got := conversationRowsWithinBudget(rows, 36)
	if len(got) != 4 || got[0].Turn != 1 || got[1].Turn != 1 || got[2].Turn != 2 || got[3].Turn != 2 {
		t.Fatalf("kept rows = %+v, want both recent turns only", got)
	}
}

func TestConversationRowsWithinBudgetRejectsOversizedNewestTurn(t *testing.T) {
	rows := []store.ListRecentAssistantMessagesRow{
		conversationBudgetRow(0, "old", "[]", "[]"),
		conversationBudgetRow(1, "newest", "[]", "[]"),
		conversationBudgetRow(1, "answer", "[]", "[]"),
	}

	got := conversationRowsWithinBudget(rows, 10)
	if len(got) != 0 {
		t.Fatalf("kept rows = %+v, want none when newest turn exceeds budget", got)
	}
}

func TestConversationSerializationDefaultsToolJSON(t *testing.T) {
	calls, err := marshalStoredToolCalls([]llm.ToolCall{
		{ID: "empty", Name: "lookup"},
		{ID: "invalid", Name: "lookup", Input: json.RawMessage(`{invalid}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored []storedToolCall
	if err := json.Unmarshal(calls, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || string(stored[0].Input) != "{}" || string(stored[1].Input) != "{}" {
		t.Fatalf("stored tool calls = %s, want empty and invalid inputs as {}", calls)
	}

	emptyCalls, err := marshalStoredToolCalls(nil)
	if err != nil || string(emptyCalls) != "[]" {
		t.Fatalf("empty tool calls = %s, %v; want []", emptyCalls, err)
	}
	emptyResults, err := marshalStoredToolResults(nil)
	if err != nil || string(emptyResults) != "[]" {
		t.Fatalf("empty tool results = %s, %v; want []", emptyResults, err)
	}
}

func conversationBudgetRow(turn int32, text, toolCalls, toolResults string) store.ListRecentAssistantMessagesRow {
	return store.ListRecentAssistantMessagesRow{
		Turn: turn, Text: text, ToolCalls: []byte(toolCalls), ToolResults: []byte(toolResults),
	}
}
