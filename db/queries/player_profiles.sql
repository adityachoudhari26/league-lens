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
