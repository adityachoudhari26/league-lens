-- name: DashboardLeague :one
SELECT l.id,l.provider,l.external_league_id,l.season,l.name,
 (SELECT max(cl.last_successful_sync_at) FROM connection_leagues cl
  JOIN connections c ON c.id=cl.connection_id
  WHERE cl.league_season_id=l.id AND cl.user_id=$1 AND cl.detached_at IS NULL AND c.status<>'disabled')::timestamptz AS last_successful_sync_at,
 ARRAY(SELECT ut.team_id FROM user_teams ut JOIN connections c ON c.id=ut.connection_id
  WHERE ut.league_season_id=l.id AND ut.user_id=$1 AND c.status<>'disabled'
  AND EXISTS (SELECT 1 FROM connection_leagues selected
   WHERE selected.connection_id=ut.connection_id AND selected.league_season_id=ut.league_season_id
   AND selected.user_id=ut.user_id AND selected.detached_at IS NULL)
  ORDER BY ut.team_id)::uuid[] AS selected_team_ids
FROM league_seasons l
WHERE l.id=$2 AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=l.id AND cl.user_id=$1 AND cl.detached_at IS NULL AND c.status<>'disabled'
);

-- name: DashboardLeagues :many
SELECT l.id,l.provider,l.external_league_id,l.season,l.name,
 (SELECT max(cl.last_successful_sync_at) FROM connection_leagues cl
  JOIN connections c ON c.id=cl.connection_id
  WHERE cl.league_season_id=l.id AND cl.user_id=$1 AND cl.detached_at IS NULL AND c.status<>'disabled')::timestamptz AS last_successful_sync_at,
 ARRAY(SELECT ut.team_id FROM user_teams ut JOIN connections c ON c.id=ut.connection_id
  WHERE ut.league_season_id=l.id AND ut.user_id=$1 AND c.status<>'disabled'
  AND EXISTS (SELECT 1 FROM connection_leagues selected
   WHERE selected.connection_id=ut.connection_id AND selected.league_season_id=ut.league_season_id
   AND selected.user_id=ut.user_id AND selected.detached_at IS NULL)
  ORDER BY ut.team_id)::uuid[] AS selected_team_ids
FROM league_seasons l
WHERE (l.id>$2 OR $2::uuid IS NULL)
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=l.id AND cl.user_id=$1 AND cl.detached_at IS NULL AND c.status<>'disabled'
) ORDER BY l.id LIMIT $3;

-- name: DashboardTeam :one
SELECT t.* FROM teams t WHERE t.id=$1 AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=t.league_season_id AND cl.user_id=$2 AND cl.detached_at IS NULL AND c.status<>'disabled'
);

-- name: DashboardRoster :one
SELECT s.* FROM team_snapshots s JOIN sync_runs r ON r.id=s.sync_run_id
WHERE s.team_id=$1 AND r.status='succeeded'
AND (s.period=$2 OR ($2::integer=0 AND s.origin='observed'))
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=s.league_season_id AND cl.user_id=$3 AND cl.detached_at IS NULL AND c.status<>'disabled'
)
ORDER BY s.period DESC,s.observed_at DESC,(s.origin='provider_history') DESC,s.id DESC LIMIT 1;

-- name: DashboardMatchupPeriods :many
SELECT DISTINCT m.period FROM matchups m WHERE m.league_season_id=$1
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=m.league_season_id AND cl.user_id=$2 AND cl.detached_at IS NULL AND c.status<>'disabled'
) ORDER BY m.period;
