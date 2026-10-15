-- +goose Up
-- The live loop's last read of each lineup row; the baseline for detecting point changes.
CREATE TABLE live_lineup_observations (
 matchup_id uuid NOT NULL,
 team_id uuid NOT NULL,
 player_id uuid NOT NULL,
 points numeric(14,4),
 touchdowns integer,
 field_goals integer,
 observed_at timestamptz NOT NULL,
 PRIMARY KEY (matchup_id, team_id, player_id),
 FOREIGN KEY (matchup_id, team_id, player_id) REFERENCES matchup_lineups ON DELETE CASCADE
);

-- One row per starter per live tick whose points or known TD/FG counts changed.
CREATE TABLE lineup_point_changes (
 matchup_id uuid NOT NULL REFERENCES matchups(id) ON DELETE CASCADE,
 team_id uuid NOT NULL REFERENCES teams(id),
 player_id uuid NOT NULL REFERENCES players(id),
 observed_at timestamptz NOT NULL,
 points_before numeric(14,4) NOT NULL,
 points_after numeric(14,4) NOT NULL,
 touchdowns_before integer,
 touchdowns_after integer,
 field_goals_before integer,
 field_goals_after integer,
 PRIMARY KEY (matchup_id, team_id, player_id, observed_at)
);

-- +goose Down
DROP TABLE lineup_point_changes;
DROP TABLE live_lineup_observations;
