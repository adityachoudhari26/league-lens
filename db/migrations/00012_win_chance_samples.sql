-- +goose Up
-- One row per live tick that changed a matchup's scores, live projections, or win chance.
CREATE TABLE win_chance_samples (
 matchup_id uuid NOT NULL REFERENCES matchups(id) ON DELETE CASCADE,
 observed_at timestamptz NOT NULL,
 team_a_win_chance numeric(5,4) NOT NULL CHECK (team_a_win_chance BETWEEN 0 AND 1),
 score_a numeric(14,4) NOT NULL,
 score_b numeric(14,4) NOT NULL,
 projected_a numeric(14,4) NOT NULL,
 projected_b numeric(14,4) NOT NULL,
 PRIMARY KEY (matchup_id, observed_at)
);

-- +goose Down
DROP TABLE win_chance_samples;
