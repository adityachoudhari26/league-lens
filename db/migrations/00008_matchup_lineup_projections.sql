-- +goose Up
-- The provider's latest league-scored projection for the player in the
-- matchup, replaced on every sync with points. NULL means no projection; it is
-- never a stand-in for points.
ALTER TABLE matchup_lineups ADD COLUMN projected_points numeric(14,4);

-- +goose Down
ALTER TABLE matchup_lineups DROP COLUMN projected_points;
