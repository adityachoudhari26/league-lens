-- name: ListConnectionsForUser :many
SELECT id,provider,status
FROM connections
WHERE user_id=$1
ORDER BY created_at,id;

-- name: ListConnectionLeaguesForUser :many
SELECT cl.connection_id,cl.league_season_id,l.external_league_id,l.name,l.season,cl.last_successful_sync_at
FROM connection_leagues cl
JOIN connections c ON c.id=cl.connection_id
JOIN league_seasons l ON l.id=cl.league_season_id
WHERE c.user_id=$1 AND cl.user_id=$1 AND cl.detached_at IS NULL
ORDER BY c.created_at,l.season DESC,l.name;

-- name: IsLeagueAttachedForUser :one
SELECT EXISTS(
 SELECT 1 FROM connection_leagues cl
 JOIN connections c ON c.id=cl.connection_id
 JOIN league_seasons l ON l.id=cl.league_season_id
 WHERE cl.user_id=$1 AND c.user_id=$1 AND c.provider=$2
  AND l.external_league_id=$3 AND l.season=$4 AND cl.detached_at IS NULL
);

-- name: FindAttachedLeagueIDForConnection :one
SELECT l.id
FROM connection_leagues cl
JOIN league_seasons l ON l.id=cl.league_season_id
WHERE cl.connection_id=$1 AND cl.user_id=$2 AND l.external_league_id=$3
 AND l.season=$4 AND cl.detached_at IS NULL;
