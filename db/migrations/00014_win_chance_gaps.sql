-- +goose Up
CREATE TABLE win_chance_gaps (
 matchup_id uuid NOT NULL REFERENCES matchups(id) ON DELETE CASCADE,
 started_at timestamptz NOT NULL,
 ended_at timestamptz NOT NULL CHECK (ended_at > started_at),
 filled_at timestamptz,
 PRIMARY KEY (matchup_id, started_at)
);

CREATE TABLE win_chance_gap_starters (
 matchup_id uuid NOT NULL,
 started_at timestamptz NOT NULL,
 team_id uuid NOT NULL REFERENCES teams(id),
 player_id uuid NOT NULL REFERENCES players(id),
 points_before numeric(14,4) NOT NULL,
 points_after numeric(14,4) NOT NULL,
 touchdowns_before integer,
 touchdowns_after integer,
 field_goals_before integer,
 field_goals_after integer,
 PRIMARY KEY (matchup_id, started_at, team_id, player_id),
 FOREIGN KEY (matchup_id, started_at)
  REFERENCES win_chance_gaps(matchup_id, started_at) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE win_chance_gap_starters;
DROP TABLE win_chance_gaps;
