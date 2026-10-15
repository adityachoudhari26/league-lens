-- name: OverviewTeams :many
-- Only selected teams, enabled connections, and the owner's latest imported season.
SELECT t.id,t.name,l.id AS league_id,l.name AS league_name,l.provider,l.season,l.settings_json,
 s.id AS snapshot_id,coalesce(s.period,0)::integer AS period,coalesce(s.coverage,'')::text AS coverage,s.observed_at
FROM user_teams ut
JOIN connections c ON c.id=ut.connection_id AND c.status<>'disabled'
JOIN teams t ON t.id=ut.team_id
JOIN league_seasons l ON l.id=ut.league_season_id
LEFT JOIN LATERAL (
 SELECT ts.id,ts.period,ts.coverage,ts.observed_at FROM team_snapshots ts
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE ts.team_id=t.id AND ts.origin='observed'
 ORDER BY ts.period DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
) s ON true
WHERE ut.user_id=$1 AND l.season=(
 SELECT max(ls.season) FROM user_teams selected
 JOIN connections active ON active.id=selected.connection_id AND active.status<>'disabled'
 JOIN league_seasons ls ON ls.id=selected.league_season_id
 WHERE selected.user_id=$1
)
ORDER BY l.name,t.name,t.id LIMIT 101;

-- name: OverviewTeamMatchup :one
-- Match the latest observed scoring week, not the greatest (possibly future) round.
SELECT m.*,a.name AS team_a_name,b.name AS team_b_name FROM matchups m
JOIN teams a ON a.id=m.team_a_id
LEFT JOIN teams b ON b.id=m.team_b_id
WHERE (m.team_a_id = @team_id OR m.team_b_id = @team_id)
AND m.scoring_periods_json @> jsonb_build_array(@period::integer)
AND EXISTS (
 SELECT 1 FROM user_teams ut JOIN connections c ON c.id=ut.connection_id
 WHERE ut.team_id = @team_id AND ut.user_id = @user_id AND c.status<>'disabled'
)
ORDER BY m.fetched_at DESC,m.id DESC LIMIT 1;
