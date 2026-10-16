-- name: AssistantInjuryOwnerRosterPlayerIDs :many
-- Players on the team's latest successful observed snapshot.
WITH latest_snapshot AS (
 SELECT ts.id
 FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.league_season_id = @league_season_id AND ts.team_id = @team_id AND ts.origin='observed'
 ORDER BY ts.observed_at DESC,ts.id DESC
 LIMIT 1
)
SELECT e.player_id
FROM latest_snapshot snapshot
JOIN roster_entries e ON e.snapshot_id=snapshot.id
ORDER BY e.player_id;

-- name: AssistantInjuryPlayers :many
-- Requested players that exist, with one ESPN and one Sleeper ID (provider-supplied
-- before dataset mappings) and the NFL team from their most recent successful roster entry.
SELECT p.id AS player_id,p.display_name,
 coalesce(espn.external_id,'')::text AS espn_id,
 coalesce(sleeper.external_id,'')::text AS sleeper_id,
 coalesce(team.pro_team,'')::text AS pro_team
FROM players p
LEFT JOIN LATERAL (
 SELECT x.external_id FROM player_external_ids x
 WHERE x.player_id=p.id AND x.provider='espn'
 ORDER BY x.mapping_source='provider' DESC,x.external_id
 LIMIT 1
) espn ON true
LEFT JOIN LATERAL (
 SELECT x.external_id FROM player_external_ids x
 WHERE x.player_id=p.id AND x.provider='sleeper'
 ORDER BY x.mapping_source='provider' DESC,x.external_id
 LIMIT 1
) sleeper ON true
LEFT JOIN LATERAL (
 SELECT e.pro_team
 FROM roster_entries e
 JOIN team_snapshots ts ON ts.id=e.snapshot_id
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE e.player_id=p.id AND coalesce(e.pro_team,'')<>''
 ORDER BY ts.observed_at DESC,ts.id DESC
 LIMIT 1
) team ON true
WHERE p.id = ANY(@player_ids::uuid[])
ORDER BY p.display_name,p.id;
