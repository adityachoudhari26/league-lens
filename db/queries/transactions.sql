-- name: AssistantTransactionTeams :many
SELECT external_team_id, name FROM teams WHERE league_season_id = @league_season_id;

-- name: AssistantTransactionPlayers :many
SELECT x.external_id, p.display_name
FROM player_external_ids x JOIN players p ON p.id = x.player_id
WHERE x.provider = @provider AND x.external_id = ANY(@external_ids::text[]);
