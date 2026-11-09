-- +goose Up
CREATE TABLE assistant_conversations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assistant_conversations_user_updated_idx ON assistant_conversations(user_id, updated_at);

CREATE TABLE assistant_messages (
 conversation_id uuid NOT NULL REFERENCES assistant_conversations(id) ON DELETE CASCADE,
 position integer NOT NULL CHECK (position >= 0),
 turn integer NOT NULL CHECK (turn >= 0),
 role text NOT NULL CHECK (role IN ('user','assistant')),
 text text NOT NULL DEFAULT '',
 tool_calls jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(tool_calls) = 'array'),
 tool_results jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(tool_results) = 'array'),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (conversation_id, position),
 CHECK (role = 'assistant' OR tool_calls = '[]'::jsonb),
 CHECK (role = 'user' OR tool_results = '[]'::jsonb)
);

-- +goose Down
DROP TABLE assistant_messages;
DROP TABLE assistant_conversations;
