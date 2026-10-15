-- +goose Up
ALTER TABLE matchup_lineups
 ADD COLUMN touchdowns integer CHECK (touchdowns >= 0),
 ADD COLUMN field_goals integer CHECK (field_goals >= 0);

-- +goose Down
ALTER TABLE matchup_lineups DROP COLUMN field_goals, DROP COLUMN touchdowns;
