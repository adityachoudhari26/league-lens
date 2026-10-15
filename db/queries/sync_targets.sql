-- Sync targets are the owner's attached leagues in the season the overview shows.
-- Selected teams come from earlier imports.
-- name: ListSyncTargets :many
SELECT cl.league_season_id,c.id AS connection_id,c.encrypted_credentials,c.provider,c.external_user_id,
 l.external_league_id,l.season,l.name,cl.last_successful_sync_at,
 ARRAY(SELECT t.external_team_id FROM user_teams ut JOIN teams t ON t.id=ut.team_id
  WHERE ut.user_id=cl.user_id AND ut.connection_id=cl.connection_id AND ut.league_season_id=cl.league_season_id
  AND EXISTS (SELECT 1 FROM connection_leagues attached
   WHERE attached.connection_id=ut.connection_id
   AND attached.league_season_id=ut.league_season_id
   AND attached.user_id=ut.user_id AND attached.detached_at IS NULL)
  ORDER BY t.external_team_id)::text[] AS selected_team_ids
FROM connection_leagues cl
JOIN connections c ON c.id=cl.connection_id
JOIN league_seasons l ON l.id=cl.league_season_id
WHERE cl.user_id=$1 AND cl.detached_at IS NULL AND c.status<>'disabled'
AND l.season=(
 SELECT max(ls.season) FROM user_teams selected
 JOIN connections active ON active.id=selected.connection_id AND active.status<>'disabled'
 JOIN connection_leagues attached ON attached.connection_id=selected.connection_id
  AND attached.league_season_id=selected.league_season_id
  AND attached.user_id=selected.user_id AND attached.detached_at IS NULL
 JOIN league_seasons ls ON ls.id=selected.league_season_id
 WHERE selected.user_id=$1
)
ORDER BY l.provider,l.name,cl.connection_id;
