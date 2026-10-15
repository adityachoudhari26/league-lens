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

-- name: UpdateLiveLineupEntry :execrows
-- Nil counts mean the provider couldn't supply them this tick, so keep the saved values.
UPDATE matchup_lineups ml SET points = @points,
touchdowns = coalesce(@touchdowns, ml.touchdowns), field_goals = coalesce(@field_goals, ml.field_goals)
FROM player_external_ids x
WHERE ml.matchup_id = @matchup_id AND ml.team_id = @team_id AND ml.player_id = x.player_id
AND x.provider = @provider AND x.external_id = @external_player_id;
