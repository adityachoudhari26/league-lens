-- name: MatchupDetailOwnerTeam :one
-- The owner's selected team in the league (user_teams allows at most one) and
-- the scoring week of its latest observed snapshot (0 when none).
SELECT ut.team_id,l.season,
 coalesce((SELECT ts.period FROM team_snapshots ts
  JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
  WHERE ts.team_id=ut.team_id AND ts.origin='observed'
  ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1),0)::integer AS observed_period
FROM user_teams ut
JOIN connections c ON c.id=ut.connection_id AND c.status<>'disabled'
JOIN connection_leagues cl ON cl.connection_id=ut.connection_id
 AND cl.league_season_id=ut.league_season_id AND cl.user_id=ut.user_id AND cl.detached_at IS NULL
JOIN league_seasons l ON l.id=ut.league_season_id
WHERE ut.user_id = @user_id AND ut.league_season_id = @league_season_id;

-- name: MatchupDetailRound :one
-- A period of 0 means omitted: the round containing the observed scoring week,
-- matching OverviewTeamMatchup.
SELECT m.* FROM matchups m
WHERE m.league_season_id = @league_season_id
AND (m.team_a_id = @team_id OR m.team_b_id = @team_id)
AND CASE WHEN @period::integer = 0
 THEN m.scoring_periods_json @> jsonb_build_array(@observed_period::integer)
 ELSE m.period = @period::integer END
AND EXISTS (
 SELECT 1 FROM user_teams ut JOIN connections c ON c.id=ut.connection_id
 JOIN connection_leagues cl ON cl.connection_id=ut.connection_id
  AND cl.league_season_id=ut.league_season_id AND cl.user_id=ut.user_id AND cl.detached_at IS NULL
 WHERE ut.team_id = @team_id AND ut.user_id = @user_id AND c.status<>'disabled'
)
ORDER BY m.fetched_at DESC,m.id DESC LIMIT 1;

-- name: MatchupDetailLineup :many
-- Injury and NFL team are current: they come from the team's latest observed
-- snapshot, so players dropped since then have neither.
SELECT ml.player_id,p.display_name,ml.slot_label,ml.starter,ml.points,ml.projected_points,e.injury_status,e.pro_team
FROM matchup_lineups ml
JOIN matchups m ON m.id=ml.matchup_id
JOIN players p ON p.id=ml.player_id
LEFT JOIN LATERAL (
 SELECT ts.id FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.team_id=ml.team_id AND ts.origin='observed'
 ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
) s ON true
LEFT JOIN roster_entries e ON e.snapshot_id=s.id AND e.player_id=ml.player_id
WHERE ml.matchup_id = @matchup_id AND ml.team_id = @team_id
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=m.league_season_id AND cl.user_id = @user_id
  AND cl.detached_at IS NULL AND c.status<>'disabled'
);

-- name: MatchupDetailWinChanceSamples :many
SELECT s.observed_at,s.team_a_win_chance,s.score_a,s.score_b,s.projected_a,s.projected_b
FROM win_chance_samples s
JOIN matchups m ON m.id=s.matchup_id
WHERE s.matchup_id = @matchup_id
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=m.league_season_id AND cl.user_id = @user_id
  AND cl.detached_at IS NULL AND c.status<>'disabled'
)
ORDER BY s.observed_at;

-- name: MatchupDetailPointChanges :many
SELECT c.observed_at,c.team_id,c.player_id,p.display_name,c.points_before,c.points_after,
 c.touchdowns_before,c.touchdowns_after,c.field_goals_before,c.field_goals_after
FROM lineup_point_changes c
JOIN matchups m ON m.id=c.matchup_id
JOIN players p ON p.id=c.player_id
WHERE c.matchup_id = @matchup_id
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c2 ON c2.id=cl.connection_id
 WHERE cl.league_season_id=m.league_season_id AND cl.user_id = @user_id
  AND cl.detached_at IS NULL AND c2.status<>'disabled'
)
ORDER BY c.observed_at,c.team_id,c.player_id;
