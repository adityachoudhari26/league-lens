-- name: MatchupDifficultyOwnerRoster :many
-- The owner's latest successful observed roster, including provider-supplied Sleeper IDs.
WITH latest_snapshot AS (
 SELECT ts.id
 FROM team_snapshots ts
 JOIN sync_runs r ON r.id = ts.sync_run_id AND r.status = 'succeeded'
 WHERE ts.league_season_id = @league_season_id AND ts.team_id = @team_id AND ts.origin = 'observed'
 ORDER BY ts.observed_at DESC, ts.id DESC
 LIMIT 1
)
SELECT p.display_name AS player_name,
 coalesce(nullif(e.eligible_positions_json->>0, ''), profile.position, '')::text AS position,
 coalesce(e.pro_team, '')::text AS pro_team,
 coalesce(sleeper.external_id, '')::text AS sleeper_id
FROM latest_snapshot snapshot
JOIN roster_entries e ON e.snapshot_id = snapshot.id
JOIN players p ON p.id = e.player_id
LEFT JOIN player_profiles profile ON profile.player_id = e.player_id
LEFT JOIN LATERAL (
 SELECT x.external_id FROM player_external_ids x
 WHERE x.player_id = p.id AND x.provider = 'sleeper'
 ORDER BY x.mapping_source = 'provider' DESC, x.external_id
 LIMIT 1
) sleeper ON true
ORDER BY p.display_name, e.player_id;
