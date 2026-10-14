-- name: ListNewsPlayers :many
-- Players on the owner's selected teams' latest succeeded observed snapshot, with every known external ID.
SELECT e.player_id, coalesce(jsonb_object_agg(x.provider, x.external_id) FILTER (WHERE x.provider IS NOT NULL), '{}')::jsonb AS external_ids
FROM users u
JOIN user_teams ut ON ut.user_id=u.id
JOIN connections c ON c.id=ut.connection_id AND c.status<>'disabled'
JOIN LATERAL (
 SELECT ts.id FROM team_snapshots ts JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.team_id=ut.team_id AND ts.origin='observed'
 ORDER BY ts.period DESC, ts.observed_at DESC, ts.id DESC LIMIT 1
) s ON true
JOIN roster_entries e ON e.snapshot_id=s.id
LEFT JOIN player_external_ids x ON x.player_id=e.player_id
WHERE lower(u.email)=lower($1)
GROUP BY e.player_id;

-- name: DeleteSourceNews :exec
DELETE FROM player_news WHERE source=$1;

-- name: AddPlayerNews :exec
INSERT INTO player_news (source,source_item_id,player_id,headline,summary,url,source_label,published_at,fetched_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING;

-- name: ListRecentPlayerNews :many
SELECT * FROM player_news WHERE player_id = ANY(@player_ids::uuid[]) AND published_at >= @since ORDER BY published_at DESC, source_item_id;
