-- name: AssistantUsageCompletedWeeks :many
-- The season's newest weeks whose games have all kicked off by @as_of and that come before
-- the current week (0 when no league has synced one, leaving only the kickoff rule), newest first.
SELECT week
FROM nfl_games
WHERE season = @season AND (@current_week::integer = 0 OR week < @current_week::integer)
GROUP BY week
HAVING max(kickoff_at) <= @as_of::timestamptz
ORDER BY week DESC
LIMIT @week_limit::integer;
