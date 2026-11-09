package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const (
	conversationTurnLimit    = 10
	conversationHistoryBytes = 200_000
	staleConversationAge     = 30 * 24 * time.Hour
)

type storedToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type storedToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

type storedConversationMessage struct {
	toolCalls   []byte
	toolResults []byte
}

type conversationStore interface {
	createConversation(ctx context.Context) (string, error)
	loadConversation(ctx context.Context, conversationID string) ([]llm.Message, error)
	saveConversationTurn(ctx context.Context, conversationID string, turn []llm.Message) error
}

// conversationStore lets tests replace the database-backed store.
func (s *Service) conversationStore() conversationStore {
	if s.conversations != nil {
		return s.conversations
	}
	return s
}

// createConversation removes the owner's stale conversations before starting a new one.
func (s *Service) createConversation(ctx context.Context) (string, error) {
	var id pgtype.UUID
	err := s.DB.InTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		cutoff := pgtype.Timestamptz{
			Time: time.Now().UTC().Add(-staleConversationAge), Valid: true,
		}
		if err := queries.DeleteStaleAssistantConversations(ctx, store.DeleteStaleAssistantConversationsParams{
			UserID: owner.ID, UpdatedBefore: cutoff,
		}); err != nil {
			return err
		}
		id, err = queries.CreateAssistantConversation(ctx, owner.ID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", connect.NewError(connect.CodeFailedPrecondition, "assistant not set up")
	}
	if err != nil {
		return "", internalError("assistant conversation could not be started")
	}
	return id.String(), nil
}

// loadConversation returns the newest whole turns that fit the history budget.
func (s *Service) loadConversation(ctx context.Context, conversationID string) ([]llm.Message, error) {
	id, err := parseTradeUUID(conversationID)
	if err != nil {
		return nil, invalidArgument("conversation id is invalid")
	}

	var rows []store.ListRecentAssistantMessagesRow
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
		rows, err = queries.ListRecentAssistantMessages(ctx, store.ListRecentAssistantMessagesParams{
			ConversationID: id, TurnLimit: conversationTurnLimit,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, "conversation not found")
	}
	if err != nil {
		return nil, internalError("assistant conversation could not be loaded")
	}

	rows = conversationRowsWithinBudget(rows, conversationHistoryBytes)
	messages, err := conversationMessagesFromRows(rows)
	if err != nil {
		return nil, internalError("assistant conversation could not be loaded")
	}
	return messages, nil
}

// saveConversationTurn appends a completed turn while holding the conversation lock.
func (s *Service) saveConversationTurn(ctx context.Context, conversationID string, turn []llm.Message) error {
	if len(turn) == 0 {
		return invalidArgument("conversation turn is empty")
	}
	id, err := parseTradeUUID(conversationID)
	if err != nil {
		return invalidArgument("conversation id is invalid")
	}
	serialized, err := serializeConversationTurn(turn)
	if err != nil {
		return invalidArgument("conversation message role is invalid")
	}

	err = s.DB.InTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		locked, err := queries.LockAssistantConversation(ctx, store.LockAssistantConversationParams{
			ID: id, UserID: owner.ID,
		})
		if err != nil {
			return err
		}

		rows := make([]store.InsertAssistantMessagesParams, 0, len(turn))
		for index, message := range turn {
			rows = append(rows, store.InsertAssistantMessagesParams{
				ConversationID: id,
				Position:       locked.LastPosition + 1 + int32(index),
				Turn:           locked.LastTurn + 1,
				Role:           string(message.Role),
				Text:           message.Text,
				ToolCalls:      serialized[index].toolCalls,
				ToolResults:    serialized[index].toolResults,
			})
		}
		if _, err := queries.InsertAssistantMessages(ctx, rows); err != nil {
			return err
		}
		return queries.TouchAssistantConversation(ctx, id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return connect.NewError(connect.CodeNotFound, "conversation not found")
	}
	if err != nil {
		return internalError("assistant conversation could not be saved")
	}
	return nil
}

// conversationRowsWithinBudget keeps the newest whole turns that fit the history byte budget.
func conversationRowsWithinBudget(
	rows []store.ListRecentAssistantMessagesRow,
	budget int,
) []store.ListRecentAssistantMessagesRow {
	if len(rows) == 0 {
		return nil
	}
	turns := groupConversationRowsByTurn(rows)

	totalBytes := 0
	firstIncluded := len(turns)
	for index := len(turns) - 1; index >= 0; index-- {
		turnBytes := conversationTurnBytes(turns[index])
		if totalBytes+turnBytes > budget {
			break
		}
		totalBytes += turnBytes
		firstIncluded = index
	}

	return flattenConversationTurns(turns[firstIncluded:])
}

// groupConversationRowsByTurn keeps adjacent message rows from the same turn together.
func groupConversationRowsByTurn(
	rows []store.ListRecentAssistantMessagesRow,
) [][]store.ListRecentAssistantMessagesRow {
	turns := make([][]store.ListRecentAssistantMessagesRow, 0)
	for _, row := range rows {
		if len(turns) == 0 || turns[len(turns)-1][0].Turn != row.Turn {
			turns = append(turns, []store.ListRecentAssistantMessagesRow{row})
			continue
		}
		last := len(turns) - 1
		turns[last] = append(turns[last], row)
	}
	return turns
}

// conversationTurnBytes counts the stored payload bytes in one turn.
func conversationTurnBytes(turn []store.ListRecentAssistantMessagesRow) int {
	size := 0
	for _, row := range turn {
		size += len(row.Text) + len(row.ToolCalls) + len(row.ToolResults)
	}
	return size
}

// flattenConversationTurns preserves row order after whole-turn selection.
func flattenConversationTurns(
	turns [][]store.ListRecentAssistantMessagesRow,
) []store.ListRecentAssistantMessagesRow {
	rows := make([]store.ListRecentAssistantMessagesRow, 0)
	for _, turn := range turns {
		rows = append(rows, turn...)
	}
	return rows
}

// serializeConversationTurn converts one LLM turn into persistence payloads.
func serializeConversationTurn(turn []llm.Message) ([]storedConversationMessage, error) {
	serialized := make([]storedConversationMessage, 0, len(turn))
	for _, message := range turn {
		stored, err := serializeConversationMessage(message)
		if err != nil {
			return nil, err
		}
		serialized = append(serialized, stored)
	}
	return serialized, nil
}

// serializeConversationMessage accepts only chat roles and stores their tool payloads.
func serializeConversationMessage(message llm.Message) (storedConversationMessage, error) {
	if message.Role != llm.RoleUser && message.Role != llm.RoleAssistant {
		return storedConversationMessage{}, errors.New("unsupported assistant conversation message role")
	}
	toolCalls, err := marshalStoredToolCalls(message.ToolCalls)
	if err != nil {
		return storedConversationMessage{}, err
	}
	toolResults, err := marshalStoredToolResults(message.ToolResults)
	if err != nil {
		return storedConversationMessage{}, err
	}
	return storedConversationMessage{toolCalls: toolCalls, toolResults: toolResults}, nil
}

func marshalStoredToolCalls(calls []llm.ToolCall) ([]byte, error) {
	stored := make([]storedToolCall, 0, len(calls))
	for _, call := range calls {
		input := call.Input
		if !json.Valid(input) {
			input = json.RawMessage("{}")
		}
		stored = append(stored, storedToolCall{ID: call.ID, Name: call.Name, Input: input})
	}
	return json.Marshal(stored)
}

func marshalStoredToolResults(results []llm.ToolResult) ([]byte, error) {
	stored := make([]storedToolResult, 0, len(results))
	for _, result := range results {
		stored = append(stored, storedToolResult{
			CallID: result.CallID, Content: result.Content, IsError: result.IsError,
		})
	}
	return json.Marshal(stored)
}

// conversationMessagesFromRows converts persisted rows to messages in position order.
func conversationMessagesFromRows(rows []store.ListRecentAssistantMessagesRow) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(rows))
	for _, row := range rows {
		message, err := conversationMessageFromRow(row)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// conversationMessageFromRow restores one LLM message and marks its tool data as fetched.
func conversationMessageFromRow(row store.ListRecentAssistantMessagesRow) (llm.Message, error) {
	var storedCalls []storedToolCall
	if err := json.Unmarshal(row.ToolCalls, &storedCalls); err != nil {
		return llm.Message{}, err
	}
	var storedResults []storedToolResult
	if err := json.Unmarshal(row.ToolResults, &storedResults); err != nil {
		return llm.Message{}, err
	}
	fetchedAt := fetchedToolResultPrefix(row.CreatedAt.Time)
	return llm.Message{
		Role:        llm.Role(row.Role),
		Text:        row.Text,
		ToolCalls:   toolCallsFromStored(storedCalls),
		ToolResults: toolResultsFromStored(storedResults, fetchedAt),
	}, nil
}

// toolCallsFromStored converts persisted tool calls to provider messages.
func toolCallsFromStored(stored []storedToolCall) []llm.ToolCall {
	calls := make([]llm.ToolCall, 0, len(stored))
	for _, call := range stored {
		calls = append(calls, llm.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input})
	}
	return calls
}

// toolResultsFromStored marks each persisted result with the time its data was fetched.
func toolResultsFromStored(stored []storedToolResult, fetchedAt string) []llm.ToolResult {
	results := make([]llm.ToolResult, 0, len(stored))
	for _, result := range stored {
		results = append(results, llm.ToolResult{
			CallID: result.CallID, Content: fetchedAt + result.Content, IsError: result.IsError,
		})
	}
	return results
}

// fetchedToolResultPrefix marks stored data as old so the model should re-check live data.
func fetchedToolResultPrefix(createdAt time.Time) string {
	return "[fetched " + createdAt.UTC().Format(time.RFC3339) + "] "
}
