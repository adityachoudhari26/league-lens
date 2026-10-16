-- name: AssistantTrendingRosteredPlayers :many
WITH latest_team_snapshots AS (
 SELECT DISTINCT ON (ts.team_id) ts.id,ts.team_id
 FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.league_season_id = @league_season_id AND ts.origin='observed'
 ORDER BY ts.team_id,ts.observed_at DESC,ts.id DESC
)
SELECT x.external_id,snapshot.team_id,t.name AS team_name
FROM latest_team_snapshots snapshot
JOIN roster_entries e ON e.snapshot_id=snapshot.id
JOIN league_seasons ls ON ls.id = @league_season_id
JOIN player_external_ids x ON x.player_id=e.player_id AND x.provider=ls.provider
JOIN teams t ON t.id=snapshot.team_id
WHERE x.external_id = ANY(@external_ids::text[])
ORDER BY x.external_id,t.name;
