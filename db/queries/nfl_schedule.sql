-- name: NFLScheduleFetchedAt :one
SELECT coalesce(max(fetched_at),'epoch'::timestamptz)::timestamptz AS fetched_at FROM nfl_games WHERE season=$1;

-- name: DeleteNFLSeason :exec
DELETE FROM nfl_games WHERE season=$1;

-- name: AddNFLGame :exec
INSERT INTO nfl_games (season,week,team,opponent,kickoff_at,fetched_at) VALUES ($1,$2,$3,$4,$5,$6);

-- name: ListNFLGamesForWeek :many
SELECT team,opponent,kickoff_at FROM nfl_games WHERE season=$1 AND week=$2;
