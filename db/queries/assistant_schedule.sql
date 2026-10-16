-- name: AssistantScheduleLoaded :one
SELECT EXISTS (SELECT 1 FROM nfl_games WHERE season = @season)::boolean AS loaded;

-- name: AssistantScheduleOwnerWeek :one
-- The newest scoring week among the owner's attached teams in the season (0 when none synced),
-- the same latest-observed-snapshot week the dashboard treats as current.
SELECT coalesce(max(ts.period),0)::integer AS observed_period
FROM user_teams ut
JOIN connections c ON c.id=ut.connection_id AND c.status<>'disabled'
JOIN connection_leagues cl ON cl.connection_id=ut.connection_id
 AND cl.league_season_id=ut.league_season_id AND cl.user_id=ut.user_id AND cl.detached_at IS NULL
JOIN league_seasons l ON l.id=ut.league_season_id AND l.season = @season
JOIN team_snapshots ts ON ts.team_id=ut.team_id AND ts.origin='observed'
JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
WHERE ut.user_id = @user_id;

-- name: AssistantScheduleTeamByes :many
-- A team's bye is the first week from 1 to 18 that has games but none for that team.
-- known is false for abbreviations with no game in the season.
SELECT requested.team::text AS team,
 EXISTS (SELECT 1 FROM nfl_games game WHERE game.season = @season AND game.team=requested.team)::boolean AS known,
 coalesce(bye.week,0)::integer AS bye_week
FROM unnest(@teams::text[]) AS requested(team)
LEFT JOIN LATERAL (
 SELECT min(weeks.week) AS week
 FROM generate_series(1,18) AS weeks(week)
 WHERE EXISTS (
  SELECT 1 FROM nfl_games game
  WHERE game.season = @season AND game.week=weeks.week
 )
 AND NOT EXISTS (
  SELECT 1 FROM nfl_games game
  WHERE game.season = @season AND game.week=weeks.week AND game.team=requested.team
 )
) bye ON true
ORDER BY requested.team;

-- name: AssistantScheduleGames :many
SELECT team,week,opponent,kickoff_at FROM nfl_games
WHERE season = @season AND team = ANY(@teams::text[])
 AND week BETWEEN @from_week::integer AND @to_week::integer
ORDER BY team,week;

-- name: AssistantScheduleOwnerRoster :many
-- Players on the team's latest successful observed snapshot. Position comes from the
-- provider's eligible positions, then a saved player card; Sleeper rosters have only the latter.
WITH latest_snapshot AS (
 SELECT ts.id
 FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.league_season_id = @league_season_id AND ts.team_id = @team_id AND ts.origin='observed'
 ORDER BY ts.observed_at DESC,ts.id DESC
 LIMIT 1
)
SELECT p.display_name AS player_name,
 coalesce(nullif(e.eligible_positions_json->>0,''),profile.position,'')::text AS position,
 coalesce(e.pro_team,'')::text AS pro_team
FROM latest_snapshot snapshot
JOIN roster_entries e ON e.snapshot_id=snapshot.id
JOIN players p ON p.id=e.player_id
LEFT JOIN player_profiles profile ON profile.player_id=e.player_id
ORDER BY p.display_name,e.player_id;
