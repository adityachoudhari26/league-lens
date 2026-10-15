-- name: GetPlayerProfile :one
SELECT * FROM player_profiles WHERE player_id = @player_id;

-- name: ListPlayerProfileStats :many
SELECT * FROM player_profile_stats WHERE player_id = @player_id ORDER BY ordinal;

-- name: UpsertPlayerProfile :exec
INSERT INTO player_profiles (
    player_id,source,position,headshot_url,jersey,birth_date,height_inches,weight_lbs,college,
    draft_year,draft_round,draft_pick,position_rank,percent_rostered,percent_started,
    weekly_outlook,weekly_outlook_week,season_outlook,fetched_at
) VALUES (
    @player_id,@source,@position,@headshot_url,@jersey,@birth_date,@height_inches,@weight_lbs,@college,
    @draft_year,@draft_round,@draft_pick,@position_rank,@percent_rostered,@percent_started,
    @weekly_outlook,@weekly_outlook_week,@season_outlook,@fetched_at
)
ON CONFLICT (player_id) DO UPDATE SET
    source = excluded.source,
    position = excluded.position,
    headshot_url = excluded.headshot_url,
    jersey = excluded.jersey,
    birth_date = excluded.birth_date,
    height_inches = excluded.height_inches,
    weight_lbs = excluded.weight_lbs,
    college = excluded.college,
    draft_year = excluded.draft_year,
    draft_round = excluded.draft_round,
    draft_pick = excluded.draft_pick,
    position_rank = excluded.position_rank,
    percent_rostered = excluded.percent_rostered,
    percent_started = excluded.percent_started,
    weekly_outlook = excluded.weekly_outlook,
    weekly_outlook_week = excluded.weekly_outlook_week,
    season_outlook = excluded.season_outlook,
    fetched_at = excluded.fetched_at;

-- name: DeletePlayerProfileStats :exec
DELETE FROM player_profile_stats WHERE player_id = @player_id;

-- name: AddPlayerProfileStat :exec
INSERT INTO player_profile_stats (player_id,ordinal,label,value,rank)
VALUES (@player_id,@ordinal,@label,@value,@rank);

-- name: PlayerCardPlayer :one
-- Injury and NFL team come from the player's latest observed roster entry,
-- preferring one that reports them (Sleeper only does for selected teams).
SELECT p.id,p.display_name,p.entity_type,
 coalesce(latest.pro_team,'')::text AS pro_team,
 coalesce(latest.injury_status,'')::text AS injury_status,
 coalesce((SELECT jsonb_object_agg(x.provider,x.external_id) FROM player_external_ids x WHERE x.player_id=p.id),'{}')::jsonb AS external_ids
FROM players p
LEFT JOIN LATERAL (
 SELECT e.pro_team,e.injury_status FROM roster_entries e
 JOIN team_snapshots ts ON ts.id=e.snapshot_id AND ts.origin='observed'
 JOIN sync_runs r ON r.id=ts.sync_run_id AND r.status='succeeded'
 WHERE e.player_id=p.id
 ORDER BY (e.pro_team IS NOT NULL) DESC,(e.injury_status IS NOT NULL) DESC,ts.observed_at DESC,ts.id DESC LIMIT 1
) latest ON true
WHERE p.id = @player_id;

-- name: NextNFLGame :one
SELECT week,opponent,kickoff_at FROM nfl_games
WHERE team = @team AND kickoff_at > @now ORDER BY kickoff_at LIMIT 1;

-- name: LatestPlayerNews :one
SELECT * FROM player_news WHERE player_id = @player_id AND published_at >= @since
ORDER BY published_at DESC,source_item_id LIMIT 1;
