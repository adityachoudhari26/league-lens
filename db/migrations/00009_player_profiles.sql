-- +goose Up
-- One player's card profile from a profile source, fetched when a card opens
-- and refreshed once fetched_at is 12 hours old. Profiles are global per
-- player, not owner-scoped. NULL means the source didn't report the field.
CREATE TABLE player_profiles (
    player_id uuid PRIMARY KEY REFERENCES players(id),
    source text NOT NULL CHECK (source <> ''),
    position text CHECK (position IN ('QB','RB','WR','TE','K','DEF')),
    headshot_url text CHECK (headshot_url ~ '^https://'),
    jersey text CHECK (btrim(jersey) <> ''),
    birth_date date,
    height_inches integer CHECK (height_inches > 0),
    weight_lbs integer CHECK (weight_lbs > 0),
    college text CHECK (btrim(college) <> ''),
    draft_year integer CHECK (draft_year > 1900),
    draft_round integer CHECK (draft_round > 0),
    draft_pick integer CHECK (draft_pick > 0),
    position_rank integer CHECK (position_rank > 0),
    percent_rostered numeric(5,2) CHECK (percent_rostered BETWEEN 0 AND 100),
    percent_started numeric(5,2) CHECK (percent_started BETWEEN 0 AND 100),
    weekly_outlook text CHECK (btrim(weekly_outlook) <> ''),
    weekly_outlook_week integer CHECK (weekly_outlook_week > 0),
    season_outlook text CHECK (btrim(season_outlook) <> ''),
    fetched_at timestamptz NOT NULL,
    CHECK ((draft_year IS NULL) = (draft_round IS NULL) AND (draft_round IS NULL) = (draft_pick IS NULL)),
    CHECK ((weekly_outlook IS NULL) = (weekly_outlook_week IS NULL))
);
-- The profile's season stats in the source's display order, replaced with the
-- profile. rank is the player's league rank for the stat; NULL when unranked.
CREATE TABLE player_profile_stats (
    player_id uuid NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    label text NOT NULL CHECK (btrim(label) <> ''),
    value numeric(14,4) NOT NULL,
    rank integer CHECK (rank > 0),
    PRIMARY KEY (player_id,ordinal)
);

-- +goose Down
DROP TABLE player_profile_stats;
DROP TABLE player_profiles;
