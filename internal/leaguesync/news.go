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

	if err := r.updatePlayerNews(ctx); err != nil {
		slog.Warn("player news refresh failed; showing saved news", "error", err)
	}
}

// updatePlayerNews fetches items for rostered players and saves them atomically.
func (r *Runner) updatePlayerNews(ctx context.Context) error {
	source := r.sources.NewsSource()
	players, err := r.listNewsPlayers(ctx)
	if err != nil {
		return err
	}
	items, err := source.PlayerNews(ctx, players)
	if err != nil {
		return err
	}
	return r.savePlayerNews(ctx, source.Name(), items)
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
