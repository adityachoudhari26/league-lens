package assistant

import (
	"encoding/json"
	"reflect"
	"testing"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestConversationTurnsForDisplayMapsPlainMessages(t *testing.T) {
	rows := []store.ListAssistantMessagesRow{
		conversationDisplayRow(0, "user", "Question", "[]"),
		conversationDisplayRow(0, "assistant", "Answer", "[]"),
	}

	got, err := conversationTurnsForDisplay(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationDisplayBubbles(t, got, []displayConversationBubble{
		{role: v1.ConversationRole_CONVERSATION_ROLE_USER, text: "Question"},
		{role: v1.ConversationRole_CONVERSATION_ROLE_ASSISTANT, text: "Answer"},
	})
}

func TestConversationTurnsForDisplayJoinsToolRound(t *testing.T) {
	rows := []store.ListAssistantMessagesRow{
		conversationDisplayRow(0, "user", "Question", "[]"),
		conversationDisplayRow(0, "assistant", "Let me check. ", `[
			{"id":"one","name":"first_lookup","input":{"id":1}},
			{"id":"two","name":"second_lookup","input":{"id":2}}
		]`),
		conversationDisplayRow(0, "user", "", "[]"),
		conversationDisplayRow(0, "assistant", "Here is the answer.", "[]"),
	}
	tools := []assistantTool{
		conversationDisplayTool("first_lookup", "First activity"),
		conversationDisplayTool("second_lookup", "Second activity"),
	}

	got, err := conversationTurnsForDisplay(rows, tools)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationDisplayBubbles(t, got, []displayConversationBubble{
		{role: v1.ConversationRole_CONVERSATION_ROLE_USER, text: "Question"},
		{
			role: v1.ConversationRole_CONVERSATION_ROLE_ASSISTANT,
			text: "Let me check. Here is the answer.",
			activities: []string{
				"First activity",
				"Second activity",
			},
		},
	})
}

func TestConversationTurnsForDisplayHidesActionConfirmation(t *testing.T) {
	rows := []store.ListAssistantMessagesRow{
		conversationDisplayRow(2, "user", actionConfirmedMessage, "[]"),
		conversationDisplayRow(2, "assistant", "Dismissed the alert.", "[]"),
	}

	got, err := conversationTurnsForDisplay(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationDisplayBubbles(t, got, []displayConversationBubble{
		{role: v1.ConversationRole_CONVERSATION_ROLE_ASSISTANT, text: "Dismissed the alert."},
	})
}

func TestConversationTurnsForDisplayLabelsSyncProposal(t *testing.T) {
	rows := []store.ListAssistantMessagesRow{
		conversationDisplayRow(0, "assistant", "", `[{"id":"sync","name":"sync_leagues","input":{"only_stale":true}}]`),
	}

	got, err := conversationTurnsForDisplay(rows, buildActionTools(&fakeAssistantDashboard{}))
	if err != nil {
		t.Fatal(err)
	}
	assertConversationDisplayBubbles(t, got, []displayConversationBubble{
		{
			role:       v1.ConversationRole_CONVERSATION_ROLE_ASSISTANT,
			activities: []string{"Proposed: Refresh stale leagues"},
		},
	})
}

func TestConversationTurnsForDisplayUsesFallbackForUnknownTool(t *testing.T) {
	rows := []store.ListAssistantMessagesRow{
		conversationDisplayRow(0, "assistant", "", `[{"id":"unknown","name":"unknown_tool","input":{}}]`),
	}

	got, err := conversationTurnsForDisplay(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationDisplayBubbles(t, got, []displayConversationBubble{
		{
			role:       v1.ConversationRole_CONVERSATION_ROLE_ASSISTANT,
			activities: []string{"Checking requested data"},
		},
	})
}

func TestConversationTurnsForDisplayRejectsMalformedToolCalls(t *testing.T) {
	rows := []store.ListAssistantMessagesRow{
		conversationDisplayRow(0, "assistant", "Answer", `{malformed}`),
	}

	if _, err := conversationTurnsForDisplay(rows, nil); err == nil {
		t.Fatal("malformed tool calls returned no error")
	}
}

type displayConversationBubble struct {
	role       v1.ConversationRole
	text       string
	activities []string
}

func conversationDisplayRow(turn int32, role, text, toolCalls string) store.ListAssistantMessagesRow {
	return store.ListAssistantMessagesRow{
		Turn: turn, Role: role, Text: text, ToolCalls: json.RawMessage(toolCalls),
	}
}

func conversationDisplayTool(name, label string) assistantTool {
	return assistantTool{
		definition: llm.Tool{Name: name},
		label:      func(json.RawMessage) string { return label },
	}
}

func assertConversationDisplayBubbles(
	t *testing.T,
	got []*v1.ConversationTurn,
	want []displayConversationBubble,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("display bubbles = %d, want %d: %+v", len(got), len(want), got)
	}
	for index, bubble := range got {
		expected := want[index]
		if bubble.GetRole() != expected.role || bubble.GetText() != expected.text ||
			!reflect.DeepEqual(bubble.GetToolActivities(), expected.activities) {
			t.Errorf("bubble %d = %+v, want %+v", index, bubble, expected)
		}
	}
}
