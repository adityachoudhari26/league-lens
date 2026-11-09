-- name: CreateAssistantConversation :one
INSERT INTO assistant_conversations (user_id)
VALUES (@user_id)
RETURNING id;

-- name: DeleteStaleAssistantConversations :exec
DELETE FROM assistant_conversations
WHERE user_id = @user_id AND updated_at < @updated_before;

-- name: GetAssistantConversation :one
SELECT id
FROM assistant_conversations
WHERE id = @id AND user_id = @user_id;

-- name: LockAssistantConversation :one
SELECT
    c.id,
    COALESCE((
        SELECT max(m.position)
        FROM assistant_messages m
        WHERE m.conversation_id = c.id
    ), -1)::integer AS last_position,
    COALESCE((
        SELECT max(m.turn)
        FROM assistant_messages m
        WHERE m.conversation_id = c.id
    ), -1)::integer AS last_turn
FROM assistant_conversations c
WHERE c.id = @id AND c.user_id = @user_id
FOR UPDATE OF c;

-- name: ListRecentAssistantMessages :many
SELECT m.position, m.turn, m.role, m.text, m.tool_calls, m.tool_results, m.created_at
FROM assistant_messages m
WHERE m.conversation_id = @conversation_id
  AND m.turn > (
    SELECT COALESCE(max(latest.turn), -1) FROM assistant_messages latest
    WHERE latest.conversation_id = @conversation_id
  ) - @turn_limit::integer
ORDER BY m.position;

-- name: ListAssistantMessages :many
SELECT m.turn, m.role, m.text, m.tool_calls
FROM assistant_messages m
WHERE m.conversation_id = @conversation_id
ORDER BY m.position;

-- name: InsertAssistantMessages :copyfrom
INSERT INTO assistant_messages (
    conversation_id, position, turn, role, text, tool_calls, tool_results
) VALUES (
    @conversation_id, @position, @turn, @role, @text, @tool_calls, @tool_results
);

-- name: TouchAssistantConversation :exec
UPDATE assistant_conversations
SET updated_at = now()
WHERE id = @id;
