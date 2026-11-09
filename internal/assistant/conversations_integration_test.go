//go:build integration

package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestConversationMessagePersistenceAndRecentTurns(t *testing.T) {
	ctx := context.Background()
	db, _ := openAssistantTestDatabase(t, "assistant_conversations")
	owner := conversationCreateUser(t, db, "conversation-owner@example.test")
	conversationID := conversationCreateConversation(t, db, owner.ID)

	assertConversationLock(t, ctx, db.Queries(), owner.ID, conversationID, -1, -1)

	messages := []store.InsertAssistantMessagesParams{
		{
			ConversationID: conversationID, Position: 0, Turn: 0, Role: "user",
			Text: "Which leagues are imported?", ToolCalls: []byte("[]"), ToolResults: []byte("[]"),
		},
		{
			ConversationID: conversationID, Position: 1, Turn: 0, Role: "assistant",
			ToolCalls:   []byte(`[{"id":"call-1","name":"list_leagues","input":{}}]`),
			ToolResults: []byte("[]"),
		},
		{
			ConversationID: conversationID, Position: 2, Turn: 0, Role: "user",
			ToolCalls:   []byte("[]"),
			ToolResults: []byte(`[{"call_id":"call-1","content":"league list","is_error":false}]`),
		},
		{
			ConversationID: conversationID, Position: 3, Turn: 0, Role: "assistant",
			Text: "I found an imported league.", ToolCalls: []byte("[]"), ToolResults: []byte("[]"),
		},
		{
			ConversationID: conversationID, Position: 4, Turn: 1, Role: "user",
			Text: "Tell me about my team.", ToolCalls: []byte("[]"), ToolResults: []byte("[]"),
		},
	}
	inserted, err := db.Queries().InsertAssistantMessages(ctx, messages)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != int64(len(messages)) {
		t.Fatalf("copied %d messages, want %d", inserted, len(messages))
	}

	assertConversationLock(t, ctx, db.Queries(), owner.ID, conversationID, 4, 1)

	recent, err := db.Queries().ListRecentAssistantMessages(ctx, store.ListRecentAssistantMessagesParams{
		ConversationID: conversationID, TurnLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertConversationMessageRows(t, recent, []int32{4}, []int32{1}, []string{"user"}, []string{"Tell me about my team."})

	all, err := db.Queries().ListRecentAssistantMessages(ctx, store.ListRecentAssistantMessagesParams{
		ConversationID: conversationID, TurnLimit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertConversationMessageRows(
		t, all,
		[]int32{0, 1, 2, 3, 4},
		[]int32{0, 0, 0, 0, 1},
		[]string{"user", "assistant", "user", "assistant", "user"},
		[]string{"Which leagues are imported?", "", "", "I found an imported league.", "Tell me about my team."},
	)
	assertConversationJSON(t, all[1].ToolCalls, `[{"id":"call-1","name":"list_leagues","input":{}}]`)
	assertConversationJSON(t, all[2].ToolResults, `[{"call_id":"call-1","content":"league list","is_error":false}]`)
}

func TestConversationQueriesRequireTheOwner(t *testing.T) {
	ctx := context.Background()
	db, _ := openAssistantTestDatabase(t, "assistant_conversations")
	owner := conversationCreateUser(t, db, "conversation-owner@example.test")
	other := conversationCreateUser(t, db, "conversation-other@example.test")
	conversationID := conversationCreateConversation(t, db, owner.ID)

	_, err := db.Queries().LockAssistantConversation(ctx, store.LockAssistantConversationParams{
		ID: conversationID, UserID: other.ID,
	})
	assertConversationNoRows(t, err, "LockAssistantConversation")

	_, err = db.Queries().GetAssistantConversation(ctx, store.GetAssistantConversationParams{
		ID: conversationID, UserID: other.ID,
	})
	assertConversationNoRows(t, err, "GetAssistantConversation")
}

func TestConversationStaleCleanupIsOwnerScopedAndCascadesMessages(t *testing.T) {
	ctx := context.Background()
	db, raw := openAssistantTestDatabase(t, "assistant_conversations")
	owner := conversationCreateUser(t, db, "conversation-owner@example.test")
	other := conversationCreateUser(t, db, "conversation-other@example.test")
	staleOwner := conversationCreateConversation(t, db, owner.ID)
	freshOwner := conversationCreateConversation(t, db, owner.ID)
	staleOther := conversationCreateConversation(t, db, other.ID)

	conversationInsertMessage(t, ctx, db.Queries(), staleOwner)
	conversationInsertMessage(t, ctx, db.Queries(), freshOwner)
	conversationInsertMessage(t, ctx, db.Queries(), staleOther)

	cutoff := time.Now().UTC().Add(-time.Hour)
	conversationSetUpdatedAt(t, ctx, raw, staleOwner, cutoff.Add(-time.Hour))
	conversationSetUpdatedAt(t, ctx, raw, freshOwner, cutoff.Add(time.Hour))
	conversationSetUpdatedAt(t, ctx, raw, staleOther, cutoff.Add(-time.Hour))

	err := db.Queries().DeleteStaleAssistantConversations(ctx, store.DeleteStaleAssistantConversationsParams{
		UserID: owner.ID,
		UpdatedBefore: pgtype.Timestamptz{
			Time: cutoff, Valid: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertConversationNoRowsForOwner(t, ctx, db.Queries(), owner.ID, staleOwner)
	assertConversationExists(t, ctx, db.Queries(), owner.ID, freshOwner)
	assertConversationExists(t, ctx, db.Queries(), other.ID, staleOther)
	assertConversationMessageCount(t, ctx, raw, staleOwner, 0)
	assertConversationMessageCount(t, ctx, raw, freshOwner, 1)
	assertConversationMessageCount(t, ctx, raw, staleOther, 1)
}

func TestConversationMessageChecksEnforceToolRole(t *testing.T) {
	ctx := context.Background()
	db, raw := openAssistantTestDatabase(t, "assistant_conversations")
	owner := conversationCreateUser(t, db, "conversation-owner@example.test")
	conversationID := conversationCreateConversation(t, db, owner.ID)

	assertConversationConstraintRejects(
		t, ctx, raw,
		`INSERT INTO assistant_messages (conversation_id, position, turn, role, tool_calls, tool_results)
		 VALUES ($1, 0, 0, 'assistant', '[]'::jsonb, '[{"call_id":"call-1","content":"result","is_error":false}]'::jsonb)`,
		conversationID.String(),
	)
	assertConversationConstraintRejects(
		t, ctx, raw,
		`INSERT INTO assistant_messages (conversation_id, position, turn, role, tool_calls, tool_results)
		 VALUES ($1, 1, 0, 'user', '[{"id":"call-1","name":"list_leagues","input":{}}]'::jsonb, '[]'::jsonb)`,
		conversationID.String(),
	)
}

func conversationCreateUser(t *testing.T, db *database.DB, email string) store.User {
	t.Helper()
	user, err := db.Queries().CreateUser(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func conversationCreateConversation(t *testing.T, db *database.DB, userID pgtype.UUID) pgtype.UUID {
	t.Helper()
	conversationID, err := db.Queries().CreateAssistantConversation(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return conversationID
}

func conversationInsertMessage(
	t *testing.T,
	ctx context.Context,
	queries *store.Queries,
	conversationID pgtype.UUID,
) {
	t.Helper()
	_, err := queries.InsertAssistantMessages(ctx, []store.InsertAssistantMessagesParams{{
		ConversationID: conversationID,
		Role:           "user",
		Text:           "saved message",
		ToolCalls:      []byte("[]"),
		ToolResults:    []byte("[]"),
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func conversationSetUpdatedAt(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	conversationID pgtype.UUID,
	updatedAt time.Time,
) {
	t.Helper()
	if _, err := raw.ExecContext(
		ctx,
		"UPDATE assistant_conversations SET updated_at = $1 WHERE id = $2",
		updatedAt,
		conversationID.String(),
	); err != nil {
		t.Fatal(err)
	}
}

func assertConversationLock(
	t *testing.T,
	ctx context.Context,
	queries *store.Queries,
	userID, conversationID pgtype.UUID,
	wantPosition, wantTurn int32,
) {
	t.Helper()
	got, err := queries.LockAssistantConversation(ctx, store.LockAssistantConversationParams{
		ID: conversationID, UserID: userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.LastPosition != wantPosition || got.LastTurn != wantTurn {
		t.Fatalf("conversation lock = position %d, turn %d; want position %d, turn %d",
			got.LastPosition, got.LastTurn, wantPosition, wantTurn)
	}
}

func assertConversationMessageRows(
	t *testing.T,
	got []store.ListRecentAssistantMessagesRow,
	positions, turns []int32,
	roles, texts []string,
) {
	t.Helper()
	if len(got) != len(positions) || len(got) != len(turns) || len(got) != len(roles) || len(got) != len(texts) {
		t.Fatalf("listed %d message rows, want positions %v", len(got), positions)
	}
	for i, row := range got {
		if row.Position != positions[i] || row.Turn != turns[i] || row.Role != roles[i] || row.Text != texts[i] {
			t.Fatalf("message row %d = %+v, want position %d, turn %d, role %q, text %q",
				i, row, positions[i], turns[i], roles[i], texts[i])
		}
	}
}

func assertConversationJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSONB value = %s, want %s", got, want)
	}
}

func assertConversationNoRows(t *testing.T, err error, operation string) {
	t.Helper()
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("%s error = %v, want pgx.ErrNoRows", operation, err)
	}
}

func assertConversationNoRowsForOwner(
	t *testing.T,
	ctx context.Context,
	queries *store.Queries,
	userID, conversationID pgtype.UUID,
) {
	t.Helper()
	_, err := queries.GetAssistantConversation(ctx, store.GetAssistantConversationParams{
		ID: conversationID, UserID: userID,
	})
	assertConversationNoRows(t, err, "GetAssistantConversation")
}

func assertConversationExists(
	t *testing.T,
	ctx context.Context,
	queries *store.Queries,
	userID, conversationID pgtype.UUID,
) {
	t.Helper()
	got, err := queries.GetAssistantConversation(ctx, store.GetAssistantConversationParams{
		ID: conversationID, UserID: userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != conversationID {
		t.Fatalf("conversation ID = %v, want %v", got, conversationID)
	}
}

func assertConversationMessageCount(
	t *testing.T,
	ctx context.Context,
	raw *sql.DB,
	conversationID pgtype.UUID,
	want int64,
) {
	t.Helper()
	var got int64
	if err := raw.QueryRowContext(
		ctx,
		"SELECT count(*) FROM assistant_messages WHERE conversation_id = $1",
		conversationID.String(),
	).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("message count for conversation %s = %d, want %d", conversationID, got, want)
	}
}

func assertConversationConstraintRejects(t *testing.T, ctx context.Context, raw *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := raw.ExecContext(ctx, query, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("assistant message insert error = %v, want check_violation (23514)", err)
	}
}
