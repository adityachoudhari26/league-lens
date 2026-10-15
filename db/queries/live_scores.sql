-- name: ListLiveMatchupCandidates :many
-- The owner's single-week matchup for @week in each attached league of @season,
-- with the NFL teams of both sides' starters from each team's latest observed
-- snapshot ('' when a starter's team is unknown).
SELECT m.id AS matchup_id, m.league_season_id, m.period, m.external_matchup_id,
 owner_team.external_team_id AS owner_team_id, l.settings_json,
 m.team_a_id, team_a.external_team_id AS team_a_external_id,
 m.team_b_id, team_b.external_team_id AS team_b_external_id,
 ARRAY(
  SELECT DISTINCT coalesce(e.pro_team,'')
  FROM matchup_lineups ml
  LEFT JOIN LATERAL (
   SELECT ts.id FROM team_snapshots ts
   JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
   WHERE ts.team_id=ml.team_id AND ts.origin='observed'
   ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
  ) s ON true
  LEFT JOIN roster_entries e ON e.snapshot_id=s.id AND e.player_id=ml.player_id
  WHERE ml.matchup_id=m.id AND ml.starter
 )::text[] AS starter_pro_teams
FROM user_teams ut
JOIN teams owner_team ON owner_team.id=ut.team_id
JOIN connection_leagues cl ON cl.connection_id=ut.connection_id AND cl.league_season_id=ut.league_season_id
 AND cl.user_id=ut.user_id AND cl.detached_at IS NULL
JOIN connections c ON c.id=cl.connection_id AND c.status<>'disabled'
JOIN league_seasons l ON l.id=ut.league_season_id
JOIN matchups m ON m.league_season_id=ut.league_season_id AND (m.team_a_id=ut.team_id OR m.team_b_id=ut.team_id)
JOIN teams team_a ON team_a.id=m.team_a_id
LEFT JOIN teams team_b ON team_b.id=m.team_b_id
WHERE ut.user_id = @user_id AND l.season = @season AND m.scoring_periods_json = jsonb_build_array(@week::int)
ORDER BY l.provider,l.name;

-- name: UpdateLiveMatchupScores :exec
-- Status stays the full sync's job; live reads only refresh scores.
UPDATE matchups SET score_a = @score_a, score_b = @score_b, fetched_at = @fetched_at WHERE id = @matchup_id;

-- name: UpdateLiveLineupEntry :one
-- Nil counts mean the provider couldn't supply them this tick, so keep the saved values.
UPDATE matchup_lineups ml SET points = @points,
touchdowns = coalesce(@touchdowns, ml.touchdowns), field_goals = coalesce(@field_goals, ml.field_goals)
FROM player_external_ids x
WHERE ml.matchup_id = @matchup_id AND ml.team_id = @team_id AND ml.player_id = x.player_id
AND x.provider = @provider AND x.external_id = @external_player_id
RETURNING ml.player_id, ml.starter, ml.points, ml.touchdowns, ml.field_goals;

-- name: ListLiveMatchupStarters :many
-- Both sides' starters with projections and NFL team from each team's latest observed snapshot ('' when unknown).
SELECT ml.team_id, ml.projected_points, coalesce(e.pro_team,'')::text AS pro_team
FROM matchup_lineups ml
LEFT JOIN LATERAL (
 SELECT ts.id FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.team_id=ml.team_id AND ts.origin='observed'
 ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
) s ON true
LEFT JOIN roster_entries e ON e.snapshot_id=s.id AND e.player_id=ml.player_id
WHERE ml.matchup_id = @matchup_id AND ml.starter;

-- name: BackfillMatchup :one
SELECT m.status,m.is_bye,m.scoring_periods_json,l.season,l.settings_json
FROM matchups m
JOIN league_seasons l ON l.id=m.league_season_id
WHERE m.id = @matchup_id;

-- name: ListBackfillMatchupStarters :many
-- Starter identities and the latest observed roster's NFL team for play-by-play replay.
SELECT ml.team_id,ml.player_id,ml.points,ml.projected_points,
 (ml.team_id=m.team_b_id)::boolean AS team_b,
 coalesce(x.external_id,'')::text AS athlete_id,
 coalesce(e.pro_team,'')::text AS pro_team
FROM matchup_lineups ml
JOIN matchups m ON m.id=ml.matchup_id
LEFT JOIN LATERAL (
 SELECT x.external_id FROM player_external_ids x
 WHERE x.player_id=ml.player_id AND x.provider='espn' AND x.external_id ~ '^[0-9]+$'
 ORDER BY x.last_verified_at DESC NULLS LAST,x.external_id LIMIT 1
) x ON true
LEFT JOIN LATERAL (
 SELECT ts.id FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.team_id=ml.team_id AND ts.origin='observed'
 ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
) s ON true
LEFT JOIN roster_entries e ON e.snapshot_id=s.id AND e.player_id=ml.player_id
WHERE ml.matchup_id = @matchup_id AND ml.starter
ORDER BY ml.team_id,ml.player_id;

-- name: MatchupHasWinChanceHistory :one
SELECT EXISTS (
 SELECT 1 FROM win_chance_samples s WHERE s.matchup_id = @matchup_id
) OR EXISTS (
 SELECT 1 FROM lineup_point_changes c WHERE c.matchup_id = @matchup_id
) AS has_history;

-- name: LatestWinChanceSample :one
SELECT team_a_win_chance, score_a, score_b, projected_a, projected_b
FROM win_chance_samples WHERE matchup_id = @matchup_id ORDER BY observed_at DESC LIMIT 1;

-- name: InsertWinChanceSample :exec
INSERT INTO win_chance_samples (matchup_id, observed_at, team_a_win_chance, score_a, score_b, projected_a, projected_b)
VALUES (@matchup_id, @observed_at, @team_a_win_chance, @score_a, @score_b, @projected_a, @projected_b)
ON CONFLICT (matchup_id, observed_at) DO NOTHING;

-- name: ListLiveLineupObservations :many
SELECT team_id, player_id, points, touchdowns, field_goals, observed_at
FROM live_lineup_observations WHERE matchup_id = @matchup_id;

-- name: UpsertLiveLineupObservation :exec
INSERT INTO live_lineup_observations (
 matchup_id, team_id, player_id, points, touchdowns, field_goals, observed_at
)
VALUES (@matchup_id, @team_id, @player_id, @points, @touchdowns, @field_goals, @observed_at)
ON CONFLICT (matchup_id, team_id, player_id) DO UPDATE SET
 points = EXCLUDED.points,
 touchdowns = EXCLUDED.touchdowns,
 field_goals = EXCLUDED.field_goals,
 observed_at = EXCLUDED.observed_at;

-- name: InsertLineupPointChange :exec
INSERT INTO lineup_point_changes (
 matchup_id, team_id, player_id, observed_at, points_before, points_after,
 touchdowns_before, touchdowns_after, field_goals_before, field_goals_after
)
VALUES (
 @matchup_id, @team_id, @player_id, @observed_at, @points_before, @points_after,
 @touchdowns_before, @touchdowns_after, @field_goals_before, @field_goals_after
)
ON CONFLICT DO NOTHING;

-- name: InsertWinChanceGap :exec
INSERT INTO win_chance_gaps (matchup_id, started_at, ended_at)
VALUES (@matchup_id, @started_at, @ended_at)
ON CONFLICT DO NOTHING;

-- name: InsertWinChanceGapStarter :exec
INSERT INTO win_chance_gap_starters (
 matchup_id, started_at, team_id, player_id,
 points_before, points_after, touchdowns_before, touchdowns_after, field_goals_before, field_goals_after
)
VALUES (
 @matchup_id, @started_at, @team_id, @player_id,
 @points_before, @points_after, @touchdowns_before, @touchdowns_after, @field_goals_before, @field_goals_after
)
ON CONFLICT DO NOTHING;

-- name: ListUnfilledWinChanceGaps :many
SELECT matchup_id, started_at
FROM win_chance_gaps
WHERE filled_at IS NULL AND ended_at >= now() - interval '24 hours'
ORDER BY ended_at, matchup_id;

-- name: LoadWinChanceGap :one
SELECT g.started_at, g.ended_at, g.filled_at,
 m.scoring_periods_json, l.season, l.settings_json
FROM win_chance_gaps g
JOIN matchups m ON m.id=g.matchup_id
JOIN league_seasons l ON l.id=m.league_season_id
WHERE g.matchup_id = @matchup_id AND g.started_at = @started_at;

-- name: ListWinChanceGapStarters :many
-- Gap before/after values plus the starter's latest projection and ESPN/NFL identity.
SELECT gs.team_id, gs.player_id, gs.points_before, gs.points_after,
 gs.touchdowns_before, gs.touchdowns_after, gs.field_goals_before, gs.field_goals_after,
 ml.projected_points, (gs.team_id=m.team_b_id)::boolean AS team_b,
 coalesce(x.external_id,'')::text AS athlete_id,
 coalesce(e.pro_team,'')::text AS pro_team
FROM win_chance_gap_starters gs
JOIN matchups m ON m.id=gs.matchup_id
JOIN matchup_lineups ml ON ml.matchup_id=gs.matchup_id AND ml.team_id=gs.team_id AND ml.player_id=gs.player_id
LEFT JOIN LATERAL (
 SELECT x.external_id FROM player_external_ids x
 WHERE x.player_id=gs.player_id AND x.provider='espn' AND x.external_id ~ '^[0-9]+$'
 ORDER BY x.last_verified_at DESC NULLS LAST,x.external_id LIMIT 1
) x ON true
LEFT JOIN LATERAL (
 SELECT ts.id FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.team_id=gs.team_id AND ts.origin='observed'
 ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
) s ON true
LEFT JOIN roster_entries e ON e.snapshot_id=s.id AND e.player_id=gs.player_id
WHERE gs.matchup_id = @matchup_id AND gs.started_at = @started_at
ORDER BY gs.team_id,gs.player_id;

-- name: MarkWinChanceGapFilled :exec
UPDATE win_chance_gaps SET filled_at=now()
WHERE matchup_id = @matchup_id AND started_at = @started_at AND filled_at IS NULL;
