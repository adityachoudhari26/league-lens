-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL CHECK (btrim(email) <> ''),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    provider text NOT NULL CHECK (provider IN ('espn', 'sleeper')),
    external_user_id text,
    encrypted_credentials bytea,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','needs_reconnect','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, provider, user_id)
);
CREATE INDEX connections_user_idx ON connections(user_id);

CREATE TABLE league_seasons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL CHECK (provider IN ('espn','sleeper')),
    external_league_id text NOT NULL CHECK (external_league_id <> ''),
    season integer NOT NULL CHECK (season BETWEEN 1900 AND 2200),
    name text NOT NULL,
    settings_json jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(settings_json)='object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, external_league_id, season),
    UNIQUE (id, provider)
);

CREATE TABLE connection_leagues (
    connection_id uuid NOT NULL,
    league_season_id uuid NOT NULL,
    user_id uuid NOT NULL,
    provider text NOT NULL,
    last_successful_sync_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, league_season_id),
    UNIQUE (connection_id, league_season_id, user_id),
    FOREIGN KEY (connection_id,provider,user_id) REFERENCES connections(id,provider,user_id),
    FOREIGN KEY (league_season_id,provider) REFERENCES league_seasons(id,provider)
);
CREATE INDEX connection_leagues_user_idx ON connection_leagues(user_id,league_season_id);
CREATE INDEX connection_leagues_league_idx ON connection_leagues(league_season_id);

CREATE TABLE teams (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    league_season_id uuid NOT NULL REFERENCES league_seasons(id),
    external_team_id text NOT NULL CHECK (external_team_id <> ''),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (league_season_id,external_team_id),
    UNIQUE (id,league_season_id)
);

CREATE TABLE user_teams (
    user_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    league_season_id uuid NOT NULL,
    team_id uuid NOT NULL,
    PRIMARY KEY (user_id,team_id),
    FOREIGN KEY (connection_id,league_season_id,user_id) REFERENCES connection_leagues(connection_id,league_season_id,user_id),
    FOREIGN KEY (team_id,league_season_id) REFERENCES teams(id,league_season_id)
);
CREATE INDEX user_teams_team_idx ON user_teams(team_id);

-- Only create identities for players encountered during imports. Names are not unique.
CREATE TABLE players (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    entity_type text NOT NULL CHECK (entity_type IN ('person','team_defense')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE player_external_ids (
    provider text NOT NULL CHECK (provider IN ('espn','sleeper','gsis','mfl')),
    external_id text NOT NULL CHECK (external_id <> ''),
    player_id uuid NOT NULL REFERENCES players(id),
    mapping_source text NOT NULL CHECK (mapping_source IN ('provider','dynastyprocess','manual')),
    source_version text,
    last_verified_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider,external_id)
);
CREATE INDEX player_external_ids_player_idx ON player_external_ids(player_id);

CREATE TABLE sync_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id uuid NOT NULL,
    league_season_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('initial','refresh','backfill')),
    status text NOT NULL DEFAULT 'running' CHECK (status IN ('running','succeeded','failed')),
    requested_period integer CHECK (requested_period > 0),
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    error_code text,
    FOREIGN KEY (connection_id,league_season_id) REFERENCES connection_leagues(connection_id,league_season_id),
    UNIQUE (id,league_season_id),
    CHECK ((status='running' AND completed_at IS NULL AND error_code IS NULL)
        OR (status='succeeded' AND completed_at IS NOT NULL AND error_code IS NULL)
        OR (status='failed' AND completed_at IS NOT NULL AND error_code IS NOT NULL)),
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);
CREATE INDEX sync_runs_league_started_idx ON sync_runs(league_season_id,started_at DESC);
CREATE INDEX sync_runs_connection_idx ON sync_runs(connection_id);

CREATE TABLE team_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id uuid NOT NULL,
    league_season_id uuid NOT NULL,
    sync_run_id uuid NOT NULL,
    period integer NOT NULL CHECK (period > 0),
    origin text NOT NULL CHECK (origin IN ('observed','provider_history')),
    coverage text NOT NULL CHECK (coverage IN ('full','lineup','partial')),
    -- Stable provider-history key, or a unique observation key for live observations.
    source_key text NOT NULL CHECK (source_key <> ''),
    effective_at timestamptz,
    observed_at timestamptz NOT NULL DEFAULT now(),
    record_json jsonb CHECK (record_json IS NULL OR jsonb_typeof(record_json)='object'),
    content_hash text NOT NULL CHECK (content_hash ~ '^[a-f0-9]{64}$'),
    FOREIGN KEY (team_id,league_season_id) REFERENCES teams(id,league_season_id),
    FOREIGN KEY (sync_run_id,league_season_id) REFERENCES sync_runs(id,league_season_id),
    UNIQUE (team_id,period,origin,source_key)
);
CREATE INDEX team_snapshots_timeline_idx ON team_snapshots(team_id,period DESC,observed_at DESC);
CREATE INDEX team_snapshots_sync_idx ON team_snapshots(sync_run_id);

CREATE TABLE roster_entries (
    snapshot_id uuid NOT NULL REFERENCES team_snapshots(id) ON DELETE CASCADE,
    player_id uuid NOT NULL REFERENCES players(id),
    provider_slot_id text,
    slot_label text,
    eligible_positions_json jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(eligible_positions_json)='array'),
    PRIMARY KEY (snapshot_id,player_id)
);
CREATE INDEX roster_entries_player_idx ON roster_entries(player_id);

CREATE TABLE matchups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    league_season_id uuid NOT NULL REFERENCES league_seasons(id),
    external_matchup_id text NOT NULL CHECK (external_matchup_id <> ''),
    period integer NOT NULL CHECK (period > 0),
    team_a_id uuid NOT NULL,
    team_b_id uuid,
    score_a numeric(14,4),
    score_b numeric(14,4),
    status text NOT NULL CHECK (status IN ('scheduled','in_progress','final','unknown')),
    fetched_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (team_a_id,league_season_id) REFERENCES teams(id,league_season_id),
    FOREIGN KEY (team_b_id,league_season_id) REFERENCES teams(id,league_season_id),
    UNIQUE (league_season_id,period,external_matchup_id),
    CHECK (team_b_id IS NULL OR team_a_id <> team_b_id),
    CHECK (team_b_id IS NOT NULL OR score_b IS NULL)
);
CREATE INDEX matchups_team_a_idx ON matchups(team_a_id,period);
CREATE INDEX matchups_team_b_idx ON matchups(team_b_id,period);

-- +goose Down
DROP TABLE matchups;
DROP TABLE roster_entries;
DROP TABLE team_snapshots;
DROP TABLE sync_runs;
DROP TABLE player_external_ids;
DROP TABLE players;
DROP TABLE user_teams;
DROP TABLE teams;
DROP TABLE connection_leagues;
DROP TABLE league_seasons;
DROP TABLE connections;
DROP TABLE users;
