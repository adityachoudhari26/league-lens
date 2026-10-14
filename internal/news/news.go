// Package news defines source-independent player news contracts.
package news

import (
	"context"
	"time"
)

// Player is a rostered player to look up; ExternalIDs maps provider names to
// that provider's player identifier.
type Player struct {
	ID          string
	ExternalIDs map[string]string
}

// Item is a source news item mapped to a League Lens player.
type Item struct {
	PlayerID     string
	SourceItemID string
	Headline     string
	Summary      string
	URL          string
	SourceLabel  string
	PublishedAt  time.Time
}

// Source fetches recent news for identifiable players. Players a source
// cannot identify are skipped rather than treated as errors.
type Source interface {
	Name() string
	PlayerNews(ctx context.Context, players []Player) ([]Item, error)
}
