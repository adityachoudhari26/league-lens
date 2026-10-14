-- Sync targets are the owner's attached leagues in the season the overview shows.
-- Selected teams come from earlier imports.
-- name: ListSyncTargets :many
SELECT cl.connection_id,cl.league_season_id,c.provider,c.external_user_id,
 l.external_league_id,l.season,l.name,cl.last_successful_sync_at,
 ARRAY(SELECT t.external_team_id FROM user_teams ut JOIN teams t ON t.id=ut.team_id
  WHERE ut.connection_id=cl.connection_id AND ut.league_season_id=cl.league_season_id
  ORDER BY t.external_team_id)::text[] AS selected_team_ids
FROM connection_leagues cl
JOIN connections c ON c.id=cl.connection_id
JOIN league_seasons l ON l.id=cl.league_season_id
WHERE cl.user_id=$1 AND c.status<>'disabled'
AND l.season=(
 SELECT max(ls.season) FROM user_teams selected
 JOIN connections active ON active.id=selected.connection_id AND active.status<>'disabled'
 JOIN league_seasons ls ON ls.id=selected.league_season_id
 WHERE selected.user_id=$1
)
ORDER BY l.provider,l.name,cl.connection_id;
