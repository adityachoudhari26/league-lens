-- name: DismissAlert :exec
INSERT INTO alert_dismissals (user_id, fingerprint) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: RestoreAlert :exec
DELETE FROM alert_dismissals WHERE user_id = $1 AND fingerprint = $2;

-- name: ListDismissedAlerts :many
-- Only the fingerprints of alerts currently shown, so old dismissals never load.
SELECT fingerprint FROM alert_dismissals WHERE user_id = @user_id AND fingerprint = ANY(@fingerprints::text[]);
