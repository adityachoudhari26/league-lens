package leaguesync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// RefreshPlayerNews refreshes saved news for players in the current roster snapshots.
// It runs during syncs and after league imports, so new rosters get news without
// waiting for a stale sync. Failures keep existing news and never affect league syncs.
func (r *Runner) RefreshPlayerNews(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
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
	if len(players) == 0 {
		return nil
	}
	items, err := source.PlayerNews(ctx, players)
	if err != nil {
		return err
	}
	return r.savePlayerNews(ctx, source.Name(), players, items)
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

// savePlayerNews replaces the source's saved rows for the requested players in
// one transaction. News rows are global, so rows for players outside this
// refresh (other owners' rosters) are left untouched. A requested player with
// no items loses its old rows, so news that aged out of the feed disappears.
func (r *Runner) savePlayerNews(ctx context.Context, source string, players []news.Player, items []news.Item) error {
	requestedIDs, err := newsPlayerIDs(players)
	if err != nil {
		return err
	}

	fetchedAt := pgtype.Timestamptz{Time: r.now(), Valid: true}
	return r.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.DeleteSourceNewsForPlayers(ctx, store.DeleteSourceNewsForPlayersParams{
			Source: source, PlayerIds: requestedIDs,
		}); err != nil {
			return err
		}
		for _, item := range items {
			playerID, err := parsePlayerID(item.PlayerID)
			if err != nil {
				return err
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

func newsPlayerIDs(players []news.Player) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, len(players))
	for _, player := range players {
		id, err := parsePlayerID(player.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func parsePlayerID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("decode player news player ID: %w", err)
	}
	return id, nil
}
