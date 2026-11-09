//go:build integration

package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

func TestConversationStoreCreatesAndLoadsWholeTurn(t *testing.T) {
	ctx := context.Background()
	service, _, _ := conversationStoreNewService(t, "conversation-store-owner@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	assertConversationStoreIDCanonical(t, conversationID)
	assertConversationStoreTurnRoundTrips(t, ctx, service, conversationID, conversationStoreRoundTripMessages())
}

func TestConversationStoreContinuesPositionsAndTurnNumbers(t *testing.T) {
	ctx := context.Background()
	service, _, raw := conversationStoreNewService(t, "conversation-store-owner@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	conversationStoreSavePairTurns(t, ctx, service, conversationID, 2)
	got := conversationStoreLoad(t, ctx, service, conversationID)
	assertConversationStoreTexts(t, got, "question-0", "answer-0", "question-1", "answer-1")
	assertConversationStorePositions(t, ctx, raw, conversationID, []int32{0, 1, 2, 3}, []int32{0, 0, 1, 1})
}

func TestConversationStoreLoadsOnlyTenRecentTurns(t *testing.T) {
	ctx := context.Background()
	service, _, _ := conversationStoreNewService(t, "conversation-store-owner@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	conversationStoreSavePairTurns(t, ctx, service, conversationID, 12)
	got := conversationStoreLoad(t, ctx, service, conversationID)
	assertConversationStoreRecentTurns(t, got, 2, 11)
}

func TestConversationStoreEnforcesOwnerScope(t *testing.T) {
	ctx := context.Background()
	ownerService, db, raw := conversationStoreNewService(t, "conversation-owner@example.test")
	otherEmail := "conversation-other@example.test"
	conversationCreateUser(t, db, otherEmail)
	otherService := &Service{DB: db, OwnerEmail: otherEmail}
	conversationID := conversationStoreCreate(t, ctx, ownerService)
	if err := ownerService.saveConversationTurn(ctx, conversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "private question"},
	}); err != nil {
		t.Fatal(err)
	}
	assertConversationStoreOwnerIsolation(t, ctx, otherService, raw, conversationID)
}

func TestConversationStoreRejectsMalformedIDs(t *testing.T) {
	ctx := context.Background()
	service, _, _ := conversationStoreNewService(t, "conversation-store-owner@example.test")
	assertConversationStoreMalformedIDRejected(t, ctx, service)
}

func TestConversationStoreCreateCleansOnlyOwnersStaleConversations(t *testing.T) {
	ctx := context.Background()
	ownerService, db, raw := conversationStoreNewService(t, "conversation-owner@example.test")
	otherEmail := "conversation-other@example.test"
	conversationCreateUser(t, db, otherEmail)
	otherService := &Service{DB: db, OwnerEmail: otherEmail}

	staleOwnerID := conversationStoreCreateSavedConversation(t, ctx, ownerService)
	freshOwnerID := conversationStoreCreateSavedConversation(t, ctx, ownerService)
	staleOtherID := conversationStoreCreateSavedConversation(t, ctx, otherService)
	now := time.Now().UTC()
	conversationStoreSetUpdatedAt(t, ctx, raw, staleOwnerID, now.Add(-staleConversationAge-time.Hour))
	conversationStoreSetUpdatedAt(t, ctx, raw, freshOwnerID, now.Add(-time.Hour))
	conversationStoreSetUpdatedAt(t, ctx, raw, staleOtherID, now.Add(-staleConversationAge-time.Hour))

	conversationStoreCreate(t, ctx, ownerService)
	assertConversationStoreCleanupState(t, ctx, raw,
		conversationStoreState{conversationID: staleOwnerID.String(), conversations: 0, messages: 0},
		conversationStoreState{conversationID: freshOwnerID.String(), conversations: 1, messages: 1},
		conversationStoreState{conversationID: staleOtherID.String(), conversations: 1, messages: 1},
	)
}

func TestConversationStoreRejectsEmptyTurn(t *testing.T) {
	ctx := context.Background()
	service, _, _ := conversationStoreNewService(t, "conversation-store-owner@example.test")
	conversationID := conversationStoreCreate(t, ctx, service)
	err := service.saveConversationTurn(ctx, conversationID, nil)
	assertConversationStoreCode(t, err, connect.CodeInvalidArgument)
}

func TestConversationStoreMapsMissingOwner(t *testing.T) {
	ctx := context.Background()
	db, _ := openAssistantTestDatabase(t, "assistant_conversation_store")
	existingOwner := conversationCreateUser(t, db, "conversation-existing@example.test")
	conversationID := conversationCreateConversation(t, db, existingOwner.ID).String()
	service := &Service{DB: db, OwnerEmail: "conversation-missing@example.test"}
	assertConversationStoreMissingOwnerErrors(t, ctx, service, conversationID)
}

func conversationStoreNewService(
	t *testing.T,
	email string,
) (*Service, *database.DB, *sql.DB) {
	t.Helper()
	db, raw := openAssistantTestDatabase(t, "assistant_conversation_store")
	conversationCreateUser(t, db, email)
	return &Service{DB: db, OwnerEmail: email}, db, raw
}

func conversationStoreCreate(t *testing.T, ctx context.Context, service *Service) string {
	t.Helper()
	conversationID, err := service.createConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return conversationID
}

func conversationStoreLoad(
	t *testing.T,
	ctx context.Context,
	service *Service,
	conversationID string,
) []llm.Message {
	t.Helper()
	messages, err := service.loadConversation(ctx, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func conversationStoreSavePairTurns(
	t *testing.T,
	ctx context.Context,
	service *Service,
	conversationID string,
	count int,
) {
	t.Helper()
	for turn := 0; turn < count; turn++ {
		messages := []llm.Message{
			{Role: llm.RoleUser, Text: fmt.Sprintf("question-%d", turn)},
			{Role: llm.RoleAssistant, Text: fmt.Sprintf("answer-%d", turn)},
		}
		if err := service.saveConversationTurn(ctx, conversationID, messages); err != nil {
			t.Fatal(err)
		}
	}
}

func conversationStoreRoundTripMessages() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Text: "Which league is mine?"},
		{
			Role: llm.RoleAssistant, Text: "I will check.",
			ToolCalls: []llm.ToolCall{{
				ID: "call-1", Name: "list_leagues",
				Input: json.RawMessage(`{"league":"Purdy Oladokun Mahomes Dak","season":2026}`),
			}},
		},
		{
			Role: llm.RoleUser,
			ToolResults: []llm.ToolResult{{
				CallID: "call-1", Content: "Chud Ahh Team is in that league.",
			}},
		},
		{Role: llm.RoleAssistant, Text: "Your team is Chud Ahh Team."},
	}
}

func assertConversationStoreIDCanonical(t *testing.T, conversationID string) {
	t.Helper()
	id, err := parseTradeUUID(conversationID)
	if err != nil || id.String() != conversationID {
		t.Fatalf("conversation ID %q is not canonical: %v", conversationID, err)
	}
}

func assertConversationStoreTurnRoundTrips(
	t *testing.T,
	ctx context.Context,
	service *Service,
	conversationID string,
	want []llm.Message,
) {
	t.Helper()
	if err := service.saveConversationTurn(ctx, conversationID, want); err != nil {
		t.Fatal(err)
	}
	assertConversationStoreRoundTrip(t, conversationStoreLoad(t, ctx, service, conversationID), want)
}

func conversationStoreCreateSavedConversation(
	t *testing.T,
	ctx context.Context,
	service *Service,
) pgtype.UUID {
	t.Helper()
	id := conversationStoreCreate(t, ctx, service)
	if err := service.saveConversationTurn(ctx, id, []llm.Message{
		{Role: llm.RoleUser, Text: "saved message"},
	}); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseTradeUUID(id)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func conversationStoreSetUpdatedAt(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	id pgtype.UUID,
	updatedAt time.Time,
) {
	t.Helper()
	if _, err := raw.ExecContext(ctx,
		"UPDATE assistant_conversations SET updated_at = $1 WHERE id = $2",
		updatedAt, id.String()); err != nil {
		t.Fatal(err)
	}
}

type conversationStoreState struct {
	conversationID string
	conversations  int
	messages       int
}

func assertConversationStoreOwnerIsolation(
	t *testing.T,
	ctx context.Context,
	service *Service,
	raw *sql.DB,
	conversationID string,
) {
	t.Helper()
	_, err := service.loadConversation(ctx, conversationID)
	assertConversationStoreCode(t, err, connect.CodeNotFound)
	err = service.saveConversationTurn(ctx, conversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "unauthorized"},
	})
	assertConversationStoreCode(t, err, connect.CodeNotFound)
	assertConversationStoreMessageCount(t, ctx, raw, conversationID, 1)
}

func assertConversationStoreMalformedIDRejected(t *testing.T, ctx context.Context, service *Service) {
	t.Helper()
	_, err := service.loadConversation(ctx, "not-a-uuid")
	assertConversationStoreCode(t, err, connect.CodeInvalidArgument)
	err = service.saveConversationTurn(ctx, "not-a-uuid", []llm.Message{
		{Role: llm.RoleUser, Text: "question"},
	})
	assertConversationStoreCode(t, err, connect.CodeInvalidArgument)
}

func assertConversationStoreMissingOwnerErrors(
	t *testing.T,
	ctx context.Context,
	service *Service,
	conversationID string,
) {
	t.Helper()
	_, err := service.createConversation(ctx)
	assertConversationStoreCode(t, err, connect.CodeFailedPrecondition)
	_, err = service.loadConversation(ctx, conversationID)
	assertConversationStoreCode(t, err, connect.CodeNotFound)
	err = service.saveConversationTurn(ctx, conversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "question"},
	})
	assertConversationStoreCode(t, err, connect.CodeNotFound)
}

func assertConversationStoreCleanupState(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	states ...conversationStoreState,
) {
	t.Helper()
	for _, state := range states {
		assertConversationStoreConversationCount(t, ctx, raw, state.conversationID, state.conversations)
		assertConversationStoreMessageCount(t, ctx, raw, state.conversationID, state.messages)
	}
}

func assertConversationStoreTexts(t *testing.T, messages []llm.Message, want ...string) {
	t.Helper()
	if len(messages) != len(want) {
		t.Fatalf("loaded %d messages, want %d", len(messages), len(want))
	}
	for index, text := range want {
		if messages[index].Text != text {
			t.Fatalf("message %d text = %q, want %q", index, messages[index].Text, text)
		}
	}
}

func assertConversationStoreRecentTurns(
	t *testing.T,
	messages []llm.Message,
	firstTurn, lastTurn int,
) {
	t.Helper()
	wantCount := (lastTurn - firstTurn + 1) * 2
	if len(messages) != wantCount {
		t.Fatalf("loaded %d messages, want %d", len(messages), wantCount)
	}
	for turn := firstTurn; turn <= lastTurn; turn++ {
		index := (turn - firstTurn) * 2
		assertConversationStoreMessageText(t, messages[index], llm.RoleUser, fmt.Sprintf("question-%d", turn))
		assertConversationStoreMessageText(t, messages[index+1], llm.RoleAssistant, fmt.Sprintf("answer-%d", turn))
	}
}

func assertConversationStoreMessageText(t *testing.T, message llm.Message, role llm.Role, text string) {
	t.Helper()
	if message.Role != role || message.Text != text {
		t.Fatalf("message = %+v, want role %q and text %q", message, role, text)
	}
}

func assertConversationStoreRoundTrip(t *testing.T, got, want []llm.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("loaded %d messages, want %d: %+v", len(got), len(want), got)
	}
	for index := range want {
		assertConversationStoreMessage(t, index, got[index], want[index])
	}
}

func assertConversationStoreMessage(t *testing.T, index int, got, want llm.Message) {
	t.Helper()
	if got.Role != want.Role || got.Text != want.Text {
		t.Fatalf("message %d = %+v, want role %q and text %q",
			index, got, want.Role, want.Text)
	}
	assertConversationStoreToolCalls(t, index, got.ToolCalls, want.ToolCalls)
	assertConversationStoreToolResults(t, index, got.ToolResults, want.ToolResults)
}

func assertConversationStoreToolCalls(t *testing.T, index int, got, want []llm.ToolCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("message %d tool calls = %+v, want %+v", index, got, want)
	}
	for callIndex, expected := range want {
		assertConversationStoreToolCall(t, index, got[callIndex], expected)
	}
}

func assertConversationStoreToolCall(t *testing.T, index int, got, want llm.ToolCall) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || !json.Valid(got.Input) {
		t.Fatalf("message %d tool call = %+v, want ID %q, name %q and valid input JSON",
			index, got, want.ID, want.Name)
	}
	var input struct {
		League string `json:"league"`
		Season int    `json:"season"`
	}
	if err := json.Unmarshal(got.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.League != "Purdy Oladokun Mahomes Dak" || input.Season != 2026 {
		t.Fatalf("tool call input = %s", got.Input)
	}
}

func assertConversationStoreToolResults(t *testing.T, index int, got, want []llm.ToolResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("message %d tool results = %+v, want %+v", index, got, want)
	}
	for resultIndex, expected := range want {
		assertConversationStoreToolResult(t, index, got[resultIndex], expected)
	}
}

func assertConversationStoreToolResult(t *testing.T, index int, got, want llm.ToolResult) {
	t.Helper()
	if got.CallID != want.CallID || got.IsError != want.IsError ||
		!strings.HasPrefix(got.Content, "[fetched ") || !strings.HasSuffix(got.Content, want.Content) {
		t.Fatalf("message %d tool result = %+v, want fetched prefix and content ending %q",
			index, got, want.Content)
	}
}

func assertConversationStorePositions(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	conversationID string,
	wantPositions, wantTurns []int32,
) {
	t.Helper()
	rows, err := raw.QueryContext(ctx,
		"SELECT position, turn FROM assistant_messages WHERE conversation_id = $1 ORDER BY position",
		conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var positions, turns []int32
	for rows.Next() {
		var position, turn int32
		if err := rows.Scan(&position, &turn); err != nil {
			t.Fatal(err)
		}
		positions = append(positions, position)
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(positions) != fmt.Sprint(wantPositions) || fmt.Sprint(turns) != fmt.Sprint(wantTurns) {
		t.Fatalf("positions/turns = %v/%v, want %v/%v", positions, turns, wantPositions, wantTurns)
	}
}

func assertConversationStoreConversationCount(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	conversationID string,
	want int,
) {
	t.Helper()
	var got int
	if err := raw.QueryRowContext(ctx,
		"SELECT count(*) FROM assistant_conversations WHERE id = $1",
		conversationID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("conversation count for %s = %d, want %d", conversationID, got, want)
	}
}

func assertConversationStoreMessageCount(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	conversationID string,
	want int,
) {
	t.Helper()
	var got int
	if err := raw.QueryRowContext(ctx,
		"SELECT count(*) FROM assistant_messages WHERE conversation_id = $1",
		conversationID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("message count for %s = %d, want %d", conversationID, got, want)
	}
}

func assertConversationStoreCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if err == nil || connect.CodeOf(err) != want {
		t.Fatalf("error = %v, want Connect code %s", err, want)
	}
}
