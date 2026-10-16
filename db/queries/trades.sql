-- name: AssistantTradeOwnerTeam :one
SELECT ut.team_id,ls.season
FROM user_teams ut
JOIN connections c ON c.id=ut.connection_id AND c.status<>'disabled'
JOIN connection_leagues cl ON cl.connection_id=ut.connection_id
 AND cl.league_season_id=ut.league_season_id AND cl.user_id=ut.user_id AND cl.detached_at IS NULL
JOIN league_seasons ls ON ls.id=ut.league_season_id
WHERE ut.user_id = @user_id AND ut.league_season_id = @league_season_id;

-- name: AssistantTradeCurrentPlayers :many
WITH current_roster AS (
 SELECT DISTINCT ON (e.player_id)
  e.player_id,ts.team_id,e.eligible_positions_json,e.pro_team,e.injury_status
 FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 JOIN roster_entries e ON e.snapshot_id=ts.id
 WHERE ts.league_season_id = @league_season_id AND ts.origin='observed'
 ORDER BY e.player_id,ts.observed_at DESC,ts.id DESC
), latest_projections AS (
 SELECT DISTINCT ON (ml.player_id) ml.player_id,ml.projected_points
 FROM matchup_lineups ml
 JOIN matchups m ON m.id=ml.matchup_id
 WHERE m.league_season_id = @league_season_id AND ml.projected_points IS NOT NULL
 ORDER BY ml.player_id,m.period DESC,m.fetched_at DESC,m.id DESC
)
SELECT requested.player_id::text AS player_id,coalesce(p.display_name,requested.player_id::text)::text AS player_name,
 roster.team_id,coalesce(t.name,'')::text AS team_name,
 coalesce(roster.eligible_positions_json,'[]'::jsonb)::jsonb AS eligible_positions_json,
 coalesce(roster.pro_team,'')::text AS pro_team,
 coalesce(roster.injury_status,'')::text AS injury_status,
 projection.projected_points,coalesce(bye.week,0)::integer AS bye_week
FROM unnest(@player_ids::uuid[]) AS requested(player_id)
LEFT JOIN players p ON p.id=requested.player_id
LEFT JOIN current_roster roster ON roster.player_id=requested.player_id
LEFT JOIN teams t ON t.id=roster.team_id
LEFT JOIN latest_projections projection ON projection.player_id=requested.player_id
LEFT JOIN league_seasons ls ON ls.id = @league_season_id
LEFT JOIN LATERAL (
 SELECT min(weeks.week) AS week
 FROM generate_series(1,18) AS weeks(week)
 WHERE coalesce(roster.pro_team,'')<>''
 AND EXISTS (
  SELECT 1 FROM nfl_games game
  WHERE game.season=ls.season AND game.week=weeks.week
 )
 AND NOT EXISTS (
  SELECT 1 FROM nfl_games game
  WHERE game.season=ls.season AND game.week=weeks.week AND game.team=roster.pro_team
 )
) bye ON true
ORDER BY requested.player_id;

-- name: AssistantTradeOwnerRosterCounts :many
WITH latest_snapshot AS (
 SELECT ts.id
 FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.league_season_id = @league_season_id AND ts.team_id = @team_id AND ts.origin='observed'
 ORDER BY ts.observed_at DESC,ts.id DESC
 LIMIT 1
)
SELECT coalesce(nullif(e.eligible_positions_json->>0,''),'UNKNOWN')::text AS primary_position,
 count(*)::bigint AS player_count
FROM latest_snapshot snapshot
JOIN roster_entries e ON e.snapshot_id=snapshot.id
GROUP BY coalesce(nullif(e.eligible_positions_json->>0,''),'UNKNOWN')
ORDER BY primary_position;

-- name: AssistantTradeRecentPoints :many
WITH settled_periods AS (
 SELECT m.period
 FROM matchups m
 WHERE m.league_season_id = @league_season_id
 GROUP BY m.period
 HAVING bool_and(m.status='final')
 ORDER BY m.period DESC
 LIMIT 4
)
SELECT requested.player_id::text AS player_id,periods.period,(
 SELECT ml.points
 FROM matchups m
 JOIN matchup_lineups ml ON ml.matchup_id=m.id
 WHERE m.league_season_id = @league_season_id AND m.period=periods.period
  AND ml.player_id=requested.player_id
 ORDER BY m.fetched_at DESC,m.id DESC
 LIMIT 1
) AS points
FROM unnest(@player_ids::uuid[]) AS requested(player_id)
CROSS JOIN settled_periods periods
ORDER BY requested.player_id,periods.period DESC;

-- name: AssistantTradeLineupCoverage :many
WITH settled_periods AS (
 SELECT m.period
 FROM matchups m
 WHERE m.league_season_id = @league_season_id
 GROUP BY m.period
 HAVING bool_and(m.status='final')
 ORDER BY m.period DESC
 LIMIT 4
)
SELECT requested.team_id::text AS team_id,periods.period,EXISTS (
 SELECT 1 FROM matchups m
 JOIN matchup_lineups ml ON ml.matchup_id=m.id AND ml.team_id=requested.team_id
 WHERE m.league_season_id = @league_season_id AND m.period=periods.period
  AND (m.team_a_id=requested.team_id OR m.team_b_id=requested.team_id)
) AS has_lineup
FROM unnest(@team_ids::uuid[]) AS requested(team_id)
CROSS JOIN settled_periods periods
ORDER BY requested.team_id,periods.period DESC;
