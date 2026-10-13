-- +goose Up
ALTER TABLE teams ADD COLUMN record_json jsonb CHECK (jsonb_typeof(record_json)='object');
ALTER TABLE teams ADD COLUMN rank integer CHECK (rank > 0);
ALTER TABLE matchups ADD COLUMN scoring_periods_json jsonb CHECK (jsonb_typeof(scoring_periods_json)='array');
ALTER TABLE matchups ADD COLUMN is_bye boolean NOT NULL DEFAULT false CHECK (NOT is_bye OR team_b_id IS NULL);
ALTER TABLE roster_entries ALTER COLUMN eligible_positions_json DROP NOT NULL;

-- +goose Down
UPDATE roster_entries SET eligible_positions_json='[]' WHERE eligible_positions_json IS NULL;
ALTER TABLE roster_entries ALTER COLUMN eligible_positions_json SET NOT NULL;
ALTER TABLE matchups DROP COLUMN is_bye, DROP COLUMN scoring_periods_json;
ALTER TABLE teams DROP COLUMN rank, DROP COLUMN record_json;
