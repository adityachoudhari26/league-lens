-- name: StartSync :one
INSERT INTO sync_runs (connection_id,league_season_id,kind,requested_period)
VALUES ($1,$2,$3,$4) RETURNING *;

-- name: CompleteSync :one
UPDATE sync_runs SET status='succeeded',completed_at=clock_timestamp()
WHERE id=$1 AND status='running' RETURNING *;

-- name: FailSync :one
UPDATE sync_runs SET status='failed',completed_at=clock_timestamp(),error_code=$2
WHERE id=$1 AND status='running' RETURNING *;

-- name: MarkLeagueSynced :exec
UPDATE connection_leagues SET last_successful_sync_at=$3
WHERE connection_id=$1 AND league_season_id=$2;

-- name: ListSyncRunsForUser :many
SELECT s.* FROM sync_runs s
JOIN connections c ON c.id=s.connection_id
WHERE c.user_id=$1 AND s.league_season_id=$2
ORDER BY s.started_at DESC LIMIT 50;
