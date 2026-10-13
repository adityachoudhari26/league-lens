-- Serialize the brief write phase, including shared identity creation, across importers.
-- No network calls run while this transaction lock is held.
-- name: LockImportWrites :exec
SELECT pg_advisory_xact_lock(724361902);

-- name: FindSleeperConnection :one
SELECT * FROM connections WHERE user_id=$1 AND provider='sleeper' AND external_user_id=$2
ORDER BY created_at LIMIT 1;

-- name: EnsureLeagueSeason :one
INSERT INTO league_seasons (provider,external_league_id,season,name,settings_json)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (provider,external_league_id,season)
DO UPDATE SET external_league_id=EXCLUDED.external_league_id RETURNING *;

-- name: HasNewerSuccessfulSync :one
SELECT EXISTS (
 SELECT 1 FROM sync_runs newer WHERE newer.league_season_id=$1 AND newer.status='succeeded'
 AND newer.started_at > (SELECT prior.started_at FROM sync_runs prior WHERE prior.id=$2)
);

-- name: ClearSelectedTeams :exec
DELETE FROM user_teams WHERE connection_id=$1 AND league_season_id=$2;

-- name: DeleteStaleMatchups :exec
DELETE FROM matchups WHERE league_season_id=$1 AND period=$2
AND external_matchup_id <> ALL($3::text[]);

-- name: UpdateMappedPlayerName :exec
UPDATE players SET display_name=$2,updated_at=now() WHERE id=$1 AND display_name<>$2;
