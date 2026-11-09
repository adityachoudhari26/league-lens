//go:build integration

package assistant

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

func TestGetConversationReturnsAllTurns(t *testing.T) {
	ctx := context.Background()
	service, _, _ := conversationStoreNewService(t, "get-conversation-owner@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	saveConversationDisplayTurns(t, ctx, service, conversationID, 12)

	response, err := service.GetConversation(ctx, &v1.GetConversationRequest{ConversationId: conversationID})
	if err != nil {
		t.Fatal(err)
	}
	assertGetConversationAllBubbles(t, response.GetTurns())
}

func TestGetConversationEnforcesOwnerAndID(t *testing.T) {
	ctx := context.Background()
	service, db, _ := conversationStoreNewService(t, "get-conversation-owner@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	otherEmail := "get-conversation-other@example.test"
	conversationCreateUser(t, db, otherEmail)
	otherService := &Service{DB: db, OwnerEmail: otherEmail}

	assertGetConversationCode(t, otherService, conversationID, connect.CodeNotFound)
	assertGetConversationCode(t, service, "not-a-uuid", connect.CodeInvalidArgument)
}

func TestGetConversationMapsMissingOwner(t *testing.T) {
	ctx := context.Background()
	service, db, _ := conversationStoreNewService(t, "get-conversation-existing@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	missingOwner := &Service{DB: db, OwnerEmail: "get-conversation-missing@example.test"}

	assertGetConversationCode(t, missingOwner, conversationID, connect.CodeNotFound)
}

func saveConversationDisplayTurns(
	t *testing.T,
	ctx context.Context,
	service *Service,
	conversationID string,
	count int,
) {
	t.Helper()
	for turn := range count {
		messages := []llm.Message{
			{Role: llm.RoleUser, Text: fmt.Sprintf("Question %d", turn)},
			{Role: llm.RoleAssistant, Text: fmt.Sprintf("Answer %d", turn)},
		}
		if err := service.saveConversationTurn(ctx, conversationID, messages); err != nil {
			t.Fatal(err)
		}
	}
}

func assertGetConversationAllBubbles(t *testing.T, got []*v1.ConversationTurn) {
	t.Helper()
	if len(got) != 24 {
		t.Fatalf("conversation bubbles = %d, want all 24 bubbles for 12 turns", len(got))
	}
	if got[0].GetText() != "Question 0" || got[1].GetText() != "Answer 0" ||
		got[22].GetText() != "Question 11" || got[23].GetText() != "Answer 11" {
		t.Fatalf("conversation bubbles are incomplete: first=%+v last=%+v", got[:2], got[22:])
	}
}

func assertGetConversationCode(
	t *testing.T,
	service *Service,
	conversationID string,
	want connect.Code,
) {
	t.Helper()
	_, err := service.GetConversation(context.Background(), &v1.GetConversationRequest{
		ConversationId: conversationID,
	})
	if connect.CodeOf(err) != want {
		t.Fatalf("GetConversation(%q) error = %v, want code %v", conversationID, err, want)
	}
}
