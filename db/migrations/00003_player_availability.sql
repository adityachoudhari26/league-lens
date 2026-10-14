-- +goose Up
ALTER TABLE roster_entries ADD COLUMN injury_status text CHECK (injury_status IN ('active','questionable','doubtful','out','ir','unknown'));
ALTER TABLE roster_entries ADD COLUMN pro_team text CHECK (pro_team <> '');
-- One row per NFL team per scheduled game; a team with no row in a loaded week is on bye.
CREATE TABLE nfl_games (
    season integer NOT NULL,
    week integer NOT NULL CHECK (week > 0),
    team text NOT NULL CHECK (team <> ''),
    opponent text NOT NULL CHECK (opponent <> ''),
    kickoff_at timestamptz NOT NULL,
    fetched_at timestamptz NOT NULL,
    PRIMARY KEY (season,week,team)
);

-- +goose Down
DROP TABLE nfl_games;
ALTER TABLE roster_entries DROP COLUMN pro_team, DROP COLUMN injury_status;
