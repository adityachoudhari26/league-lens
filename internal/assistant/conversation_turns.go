package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// GetConversation returns saved conversation bubbles for the configured owner.
func (s *Service) GetConversation(
	ctx context.Context,
	request *v1.GetConversationRequest,
) (*v1.GetConversationResponse, error) {
	if request == nil {
		return nil, invalidArgument("conversation id is required")
	}
	rows, err := s.listConversationMessages(ctx, request.GetConversationId())
	if err != nil {
		return nil, err
	}
	turns, err := conversationTurnsForDisplay(rows, s.chatTools())
	if err != nil {
		return nil, internalError("assistant conversation could not be loaded")
	}
	return &v1.GetConversationResponse{Turns: turns}, nil
}

// listConversationMessages returns every message from an owner-scoped conversation.
func (s *Service) listConversationMessages(
	ctx context.Context,
	conversationID string,
) ([]store.ListAssistantMessagesRow, error) {
	id, err := parseTradeUUID(conversationID)
	if err != nil {
		return nil, invalidArgument("conversation id is invalid")
	}

	var rows []store.ListAssistantMessagesRow
	err = s.DB.InReadTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		if _, err := queries.GetAssistantConversation(ctx, store.GetAssistantConversationParams{
			ID: id, UserID: owner.ID,
		}); err != nil {
			return err
		}
		rows, err = queries.ListAssistantMessages(ctx, id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, "conversation not found")
	}
	if err != nil {
		return nil, internalError("assistant conversation could not be loaded")
	}
	return rows, nil
}

// conversationTurnsForDisplay rebuilds the chat bubbles represented by saved messages.
func conversationTurnsForDisplay(
	rows []store.ListAssistantMessagesRow,
	tools []assistantTool,
) ([]*v1.ConversationTurn, error) {
	var turns []*v1.ConversationTurn
	for _, turnRows := range groupAssistantMessagesByTurn(rows) {
		bubbles, err := conversationBubblesForTurn(turnRows, tools)
		if err != nil {
			return nil, err
		}
		turns = append(turns, bubbles...)
	}
	return turns, nil
}

func groupAssistantMessagesByTurn(
	rows []store.ListAssistantMessagesRow,
) [][]store.ListAssistantMessagesRow {
	var groups [][]store.ListAssistantMessagesRow
	for _, row := range rows {
		last := len(groups) - 1
		if last < 0 || groups[last][0].Turn != row.Turn {
			groups = append(groups, []store.ListAssistantMessagesRow{row})
			continue
		}
		groups[last] = append(groups[last], row)
	}
	return groups
}

func conversationBubblesForTurn(
	rows []store.ListAssistantMessagesRow,
	tools []assistantTool,
) ([]*v1.ConversationTurn, error) {
	var userBubbles []*v1.ConversationTurn
	var assistantText strings.Builder
	var activities []string
	for _, row := range rows {
		switch row.Role {
		case "user":
			if row.Text != "" && row.Text != actionConfirmedMessage {
				userBubbles = append(userBubbles, &v1.ConversationTurn{
					Role: v1.ConversationRole_CONVERSATION_ROLE_USER,
					Text: row.Text,
				})
			}
		case "assistant":
			assistantText.WriteString(row.Text)
			calls, err := storedToolCallsFromJSON(row.ToolCalls)
			if err != nil {
				return nil, err
			}
			activities = append(activities, conversationToolActivities(calls, tools)...)
		}
	}

	bubbles := userBubbles
	if assistantText.Len() > 0 || len(activities) > 0 {
		bubbles = append(bubbles, &v1.ConversationTurn{
			Role:           v1.ConversationRole_CONVERSATION_ROLE_ASSISTANT,
			Text:           assistantText.String(),
			ToolActivities: activities,
		})
	}
	return bubbles, nil
}

func storedToolCallsFromJSON(raw json.RawMessage) ([]storedToolCall, error) {
	var calls []storedToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("decode saved assistant tool calls: %w", err)
	}
	return calls, nil
}

func conversationToolActivities(calls []storedToolCall, tools []assistantTool) []string {
	activities := make([]string, 0, len(calls))
	for _, call := range calls {
		activities = append(activities, toolLabel(tools, call.Name, call.Input))
	}
	return activities
}
