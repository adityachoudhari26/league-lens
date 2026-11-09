//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

func TestChatConversationPersistsToolResultsAndActionOutcomes(t *testing.T) {
	ctx := context.Background()
	service := chatConversationService(t, "assistant-chat-conversation@example.test")
	provider := &scriptedChatProvider{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{
			ID: "unknown-call", Name: "unknown_tool", Input: json.RawMessage(`{"lookup":"value"}`),
		}}},
		{Text: "I couldn't look that up."},
		{Text: "I still don't have a live lookup."},
	}}

	firstEvents := chatConversationRunTurn(t, ctx, service, provider, "", "look it up")
	if len(firstEvents) == 0 || firstEvents[0].GetConversation() == nil {
		t.Fatalf("first chat event = %+v, want ConversationStarted", firstEvents)
	}
	conversationID := firstEvents[0].GetConversation().GetId()
	if _, err := parseTradeUUID(conversationID); err != nil {
		t.Fatalf("started conversation ID %q is invalid: %v", conversationID, err)
	}
	chatConversationRunTurn(t, ctx, service, provider, conversationID, "what did the lookup return?")
	assertChatConversationHistory(t, provider, "unknown-call")

	actionResponse, err := service.RunAssistantAction(ctx, &v1.RunAssistantActionRequest{
		ConversationId: conversationID,
		Action: &v1.AssistantAction{Action: &v1.AssistantAction_DismissAlert{
			DismissAlert: &v1.DismissAlertAction{Fingerprint: "alert:week1"},
		}},
	})
	if err != nil || actionResponse.GetSummary() != "Dismissed the alert." {
		t.Fatalf("dismiss action response=%v error=%v", actionResponse, err)
	}
	assertChatConversationActionOutcome(t, ctx, service, conversationID)
}

func chatConversationService(t *testing.T, email string) *Service {
	t.Helper()
	db, _ := openAssistantTestDatabase(t, "assistant_chat_conversation")
	conversationCreateUser(t, db, email)
	return &Service{DB: db, OwnerEmail: email, Dashboard: &fakeAssistantDashboard{}}
}

func chatConversationRunTurn(
	t *testing.T,
	ctx context.Context,
	service *Service,
	provider *scriptedChatProvider,
	conversationID, text string,
) []*v1.ChatResponse {
	t.Helper()
	var events []*v1.ChatResponse
	err := service.chat(ctx, &v1.ChatRequest{ConversationId: conversationID, Message: text},
		func(event *v1.ChatResponse) error {
			events = append(events, event)
			return nil
		},
		func(context.Context) (llm.Provider, string, error) {
			return provider, "integration-model", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func assertChatConversationHistory(t *testing.T, provider *scriptedChatProvider, callID string) {
	t.Helper()
	if len(provider.requests) != 3 {
		t.Fatalf("provider requests = %d, want three", len(provider.requests))
	}
	messages := provider.requests[2].Messages
	if len(messages) != 5 || len(messages[1].ToolCalls) != 1 ||
		messages[1].ToolCalls[0].ID != callID || messages[1].ToolCalls[0].Name != "unknown_tool" {
		t.Fatalf("follow-up request did not include the earlier tool call: %+v", messages)
	}
	if messages[0].Text != "look it up" || messages[3].Text != "I couldn't look that up." ||
		messages[4].Text != "what did the lookup return?" {
		t.Fatalf("follow-up request has unexpected turn messages: %+v", messages)
	}
	results := messages[2].ToolResults
	if len(results) != 1 || results[0].CallID != callID || !results[0].IsError ||
		!strings.HasPrefix(results[0].Content, "[fetched ") {
		t.Fatalf("follow-up request did not include the fetched tool result: %+v", results)
	}
}

func assertChatConversationActionOutcome(
	t *testing.T,
	ctx context.Context,
	service *Service,
	conversationID string,
) {
	t.Helper()
	messages, err := service.loadConversation(ctx, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 {
		t.Fatalf("stored messages = %+v, want action outcome pair", messages)
	}
	outcome := messages[len(messages)-2:]
	if outcome[0].Role != llm.RoleUser || outcome[0].Text != "I confirmed the proposed action." ||
		outcome[1].Role != llm.RoleAssistant || outcome[1].Text != "Dismissed the alert." {
		t.Fatalf("stored action outcome = %+v", outcome)
	}
}
