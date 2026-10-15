-- +goose Up
ALTER TABLE connection_leagues ADD COLUMN detached_at timestamptz;

-- +goose Down
ALTER TABLE connection_leagues DROP COLUMN detached_at;
