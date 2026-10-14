-- +goose Up
-- One team's lineup in a matchup, replaced on every sync. Points are the
-- provider's league-scored actuals for that matchup; NULL means not reported,
-- which is distinct from an actual zero.
CREATE TABLE matchup_lineups (
    matchup_id uuid NOT NULL REFERENCES matchups(id) ON DELETE CASCADE,
    team_id uuid NOT NULL REFERENCES teams(id),
    player_id uuid NOT NULL REFERENCES players(id),
    provider_slot_id text,
    slot_label text,
    starter boolean NOT NULL,
    points numeric(14,4),
    PRIMARY KEY (matchup_id,team_id,player_id)
);

-- +goose Down
DROP TABLE matchup_lineups;
