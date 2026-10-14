-- +goose Up
-- An owner has at most one selected team per league season, so a league
-- identifies "my team" without ambiguity.
ALTER TABLE user_teams ADD CONSTRAINT user_teams_one_per_league UNIQUE (user_id,league_season_id);

-- +goose Down
ALTER TABLE user_teams DROP CONSTRAINT user_teams_one_per_league;
