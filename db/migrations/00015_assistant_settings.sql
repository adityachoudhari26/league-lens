-- +goose Up
CREATE TABLE assistant_settings (
 user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 provider text NOT NULL CHECK (provider IN ('anthropic','openai_compatible')),
 base_url text NOT NULL,
 model text NOT NULL CHECK (model <> ''),
 encrypted_api_key bytea,
 api_key_hint text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (provider = 'openai_compatible' OR encrypted_api_key IS NOT NULL),
 CHECK ((provider = 'anthropic') = (base_url = '')),
 CHECK ((encrypted_api_key IS NULL) = (api_key_hint = ''))
);

-- +goose Down
DROP TABLE assistant_settings;
