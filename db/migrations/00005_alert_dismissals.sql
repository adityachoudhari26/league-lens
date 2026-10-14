-- +goose Up
-- Dismissed overview alerts, keyed by fingerprint (alert ID + week + status), so
-- a changed situation produces a new fingerprint and the alert resurfaces.
-- Stale rows are harmless: reads only match current fingerprints.
CREATE TABLE alert_dismissals (
    user_id uuid NOT NULL REFERENCES users(id),
    fingerprint text NOT NULL CHECK (fingerprint <> ''),
    dismissed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, fingerprint)
);

-- +goose Down
DROP TABLE alert_dismissals;
