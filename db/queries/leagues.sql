-- name: UpsertLeagueSeason :one
INSERT INTO league_seasons (provider,external_league_id,season,name,settings_json)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (provider,external_league_id,season)
DO UPDATE SET name=EXCLUDED.name,settings_json=EXCLUDED.settings_json,updated_at=now()
RETURNING *;

-- name: AttachLeague :exec
INSERT INTO connection_leagues (connection_id,league_season_id,user_id,provider)
VALUES ($1,$2,$3,$4) ON CONFLICT (connection_id,league_season_id) DO NOTHING;

-- name: ListLeaguesForUser :many
SELECT DISTINCT l.* FROM league_seasons l
JOIN connection_leagues cl ON cl.league_season_id=l.id
JOIN connections c ON c.id=cl.connection_id
WHERE cl.user_id=$1 AND c.status<>'disabled'
ORDER BY l.season DESC,l.name;

-- name: UpsertTeam :one
INSERT INTO teams (league_season_id,external_team_id,name,record_json,rank) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (league_season_id,external_team_id)
DO UPDATE SET name=EXCLUDED.name,record_json=EXCLUDED.record_json,rank=EXCLUDED.rank,updated_at=now() RETURNING *;

-- name: ListTeamsForUser :many
SELECT t.* FROM teams t WHERE t.league_season_id=$2
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=t.league_season_id AND cl.user_id=$1 AND c.status<>'disabled'
) ORDER BY t.name;

-- name: SelectUserTeam :exec
INSERT INTO user_teams (user_id,connection_id,league_season_id,team_id)
VALUES ($1,$2,$3,$4) ON CONFLICT (user_id,team_id)
DO UPDATE SET connection_id=EXCLUDED.connection_id;
