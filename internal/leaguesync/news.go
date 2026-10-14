package leaguesync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// refreshPlayerNews refreshes saved items from the previous roster snapshot.
// It is best-effort: failures keep existing news and never affect league syncs.
func (r *Runner) refreshPlayerNews() {
	ctx, cancel := context.WithTimeout(r.ctx, time.Minute)
	defer cancel()

	source := r.sources.NewsSource()
	if source == nil {
		slog.Warn("player news refresh failed; showing saved news", "error", fmt.Errorf("player news source unavailable"))
		return
	}
	players, err := r.listNewsPlayers(ctx)
	if err != nil {
		slog.Warn("player news refresh failed; showing saved news", "error", err)
		return
	}
	items, err := source.PlayerNews(ctx, players)
	if err != nil {
		slog.Warn("player news refresh failed; showing saved news", "error", err)
		return
	}
	if err := r.savePlayerNews(ctx, source.Name(), items); err != nil {
		slog.Warn("player news refresh failed; showing saved news", "error", err)
	}
}

func (r *Runner) listNewsPlayers(ctx context.Context) ([]news.Player, error) {
	rows, err := r.db.Queries().ListNewsPlayers(ctx, r.ownerEmail)
	if err != nil {
		return nil, err
	}
	players := make([]news.Player, 0, len(rows))
	for _, row := range rows {
		externalIDs := map[string]string{}
		if err := json.Unmarshal(row.ExternalIds, &externalIDs); err != nil {
			return nil, fmt.Errorf("decode player external IDs: %w", err)
		}
		players = append(players, news.Player{ID: row.PlayerID.String(), ExternalIDs: externalIDs})
	}
	return players, nil
}

func (r *Runner) savePlayerNews(ctx context.Context, source string, items []news.Item) error {
	fetchedAt := pgtype.Timestamptz{Time: r.now(), Valid: true}
	return r.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.DeleteSourceNews(ctx, source); err != nil {
			return err
		}
		for _, item := range items {
			var playerID pgtype.UUID
			if err := playerID.Scan(item.PlayerID); err != nil {
				return fmt.Errorf("decode player news player ID: %w", err)
			}
			if err := q.AddPlayerNews(ctx, store.AddPlayerNewsParams{
				Source:       source,
				SourceItemID: item.SourceItemID,
				PlayerID:     playerID,
				Headline:     item.Headline,
				Summary:      item.Summary,
				Url:          item.URL,
				SourceLabel:  item.SourceLabel,
				PublishedAt:  pgtype.Timestamptz{Time: item.PublishedAt, Valid: true},
				FetchedAt:    fetchedAt,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
