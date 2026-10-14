-- name: GetLatestObservedSnapshot :one
SELECT s.* FROM team_snapshots s JOIN sync_runs r ON r.id=s.sync_run_id
WHERE s.team_id=$1 AND s.period=$2 AND s.origin='observed' AND r.status='succeeded'
ORDER BY s.observed_at DESC,s.id DESC LIMIT 1;

-- name: UpsertTeamSnapshot :one
INSERT INTO team_snapshots
(team_id,league_season_id,sync_run_id,period,origin,coverage,source_key,effective_at,observed_at,record_json,content_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (team_id,period,origin,source_key)
DO UPDATE SET sync_run_id=EXCLUDED.sync_run_id,coverage=EXCLUDED.coverage,
 effective_at=EXCLUDED.effective_at,observed_at=EXCLUDED.observed_at,
 record_json=EXCLUDED.record_json,content_hash=EXCLUDED.content_hash
RETURNING *;

-- name: DeleteSnapshotEntries :exec
DELETE FROM roster_entries WHERE snapshot_id=$1;

-- name: AddRosterEntry :exec
INSERT INTO roster_entries (snapshot_id,player_id,provider_slot_id,slot_label,eligible_positions_json,injury_status,pro_team)
VALUES ($1,$2,$3,$4,$5,$6,$7);

-- name: ListSnapshotsForUser :many
SELECT s.* FROM team_snapshots s JOIN sync_runs r ON r.id=s.sync_run_id
WHERE s.team_id=$2 AND s.period=$3 AND r.status='succeeded'
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=s.league_season_id AND cl.user_id=$1 AND c.status<>'disabled'
) ORDER BY s.observed_at DESC,s.id DESC;

-- name: ListSnapshotRosterForUser :many
SELECT e.*,p.display_name,p.entity_type FROM roster_entries e
JOIN players p ON p.id=e.player_id
JOIN team_snapshots s ON s.id=e.snapshot_id
JOIN sync_runs r ON r.id=s.sync_run_id
WHERE e.snapshot_id=$2 AND r.status='succeeded'
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=s.league_season_id AND cl.user_id=$1 AND c.status<>'disabled'
) ORDER BY e.slot_label,p.display_name;

-- name: UpsertMatchup :one
INSERT INTO matchups (league_season_id,external_matchup_id,period,team_a_id,team_b_id,score_a,score_b,status,fetched_at,scoring_periods_json,is_bye)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (league_season_id,period,external_matchup_id)
DO UPDATE SET team_a_id=EXCLUDED.team_a_id,team_b_id=EXCLUDED.team_b_id,
 score_a=EXCLUDED.score_a,score_b=EXCLUDED.score_b,status=EXCLUDED.status,fetched_at=EXCLUDED.fetched_at,
 scoring_periods_json=EXCLUDED.scoring_periods_json,is_bye=EXCLUDED.is_bye
RETURNING *;

-- name: ListMatchupsForUser :many
SELECT m.* FROM matchups m WHERE m.league_season_id=$2 AND m.period=$3
AND EXISTS (
 SELECT 1 FROM connection_leagues cl JOIN connections c ON c.id=cl.connection_id
 WHERE cl.league_season_id=m.league_season_id AND cl.user_id=$1 AND c.status<>'disabled'
) ORDER BY m.external_matchup_id;
