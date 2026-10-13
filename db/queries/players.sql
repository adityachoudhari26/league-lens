-- name: FindPlayerByExternalID :one
SELECT p.* FROM players p JOIN player_external_ids e ON e.player_id=p.id
WHERE e.provider=$1 AND e.external_id=$2;

-- name: CreateEncounteredPlayer :one
INSERT INTO players (display_name,entity_type) VALUES ($1,$2) RETURNING *;

-- name: AddPlayerExternalID :one
INSERT INTO player_external_ids (provider,external_id,player_id,mapping_source,source_version)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (provider,external_id) DO NOTHING RETURNING *;

-- name: ListPlayerExternalIDs :many
SELECT * FROM player_external_ids WHERE player_id=$1 ORDER BY provider,external_id;
