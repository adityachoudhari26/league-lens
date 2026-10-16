package leaguesync

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// scheduleMaxAge bounds how often the NFL schedule is reloaded; kickoff times rarely move.
const scheduleMaxAge = 24 * time.Hour

// refreshSchedules reloads the NFL schedule behind bye and kickoff checks at
// most once a day per season. Failures only log; league syncs never wait on it.
func (r *Runner) refreshSchedules(targets []Target) {
	seen := map[int]bool{}
	for _, target := range targets {
		season := target.Selection.League.Season
		if seen[season] {
			continue
		}
		seen[season] = true
		r.RefreshNFLSchedule(r.ctx, season)
	}
}

// refreshScheduleAndNews runs a best-effort schedule refresh for the current
// season followed by a player news refresh on the Runner's context.
func (r *Runner) refreshScheduleAndNews() {
	r.RefreshNFLSchedule(r.ctx, fantasy.CurrentSeason(r.now()))
	r.RefreshPlayerNews(r.ctx)
}

// RefreshNFLSchedule loads kickoff and bye data during syncs and after league
// imports, so new installs have schedule data right away. Failures only log.
func (r *Runner) RefreshNFLSchedule(ctx context.Context, season int) {
	if err := r.refreshSchedule(ctx, season); err != nil {
		slog.Warn("nfl schedule refresh failed; bye and kickoff checks use saved data", "season", season, "error", err)
	}
}

func (r *Runner) refreshSchedule(ctx context.Context, season int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	fetchedAt, err := r.db.Queries().NFLScheduleFetchedAt(ctx, int32(season))
	if err != nil || r.now().Sub(fetchedAt.Time) < scheduleMaxAge {
		return err
	}
	games, err := r.sources.NFLSchedule(ctx, season)
	if err != nil {
		return err
	}

	now := pgtype.Timestamptz{Time: r.now(), Valid: true}
	return r.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.DeleteNFLSeason(ctx, int32(season)); err != nil {
			return err
		}
		for _, game := range games {
			err := q.AddNFLGame(ctx, store.AddNFLGameParams{
				Season:    int32(game.Season),
				Week:      int32(game.Week),
				Team:      game.Team,
				Opponent:  game.Opponent,
				KickoffAt: pgtype.Timestamptz{Time: game.KickoffAt, Valid: true},
				FetchedAt: now,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
