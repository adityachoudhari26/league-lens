-- name: GetAssistantSettings :one
SELECT * FROM assistant_settings WHERE user_id = @user_id;

-- name: UpsertAssistantSettings :exec
INSERT INTO assistant_settings (
    user_id, provider, base_url, model, encrypted_api_key, api_key_hint
) VALUES (
    @user_id, @provider, @base_url, @model, @encrypted_api_key, @api_key_hint
)
ON CONFLICT (user_id) DO UPDATE SET
    provider = excluded.provider,
    base_url = excluded.base_url,
    model = excluded.model,
    encrypted_api_key = excluded.encrypted_api_key,
    api_key_hint = excluded.api_key_hint,
    updated_at = now();

-- name: DeleteAssistantSettings :exec
DELETE FROM assistant_settings WHERE user_id = @user_id;
