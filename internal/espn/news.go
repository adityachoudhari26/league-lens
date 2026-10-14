package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/news"
)

const (
	playerNewsURL     = "https://site.api.espn.com/apis/fantasy/v2/games/ffl/news/players"
	playerNewsLimit   = 200
	playerNewsBatch   = 25
	playerNewsMaxBody = 16 << 20
)

// NewsSource reads public RotoWire player blurbs from ESPN's fantasy feed.
type NewsSource struct {
	HTTP *http.Client
}

type playerNewsResponse struct {
	Feed []playerNewsRecord `json:"feed"`
}

type playerNewsRecord struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Headline  string `json:"headline"`
	Story     string `json:"story"`
	Published string `json:"published"`
	Premium   bool   `json:"premium"`
	PlayerID  int64  `json:"playerId"`
}

// Name is the stable storage key for ESPN news.
func (NewsSource) Name() string { return "espn" }

// PlayerNews fetches public news for ESPN-identifiable players in batches of
// 25. ESPN's feed combines selected players and caps each batch at 200 items.
func (s NewsSource) PlayerNews(ctx context.Context, players []news.Player) ([]news.Item, error) {
	playerIDs, externalIDs := espnPlayerIDs(players)
	if len(externalIDs) == 0 {
		return nil, nil
	}

	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var items []news.Item
	for start := 0; start < len(externalIDs); start += playerNewsBatch {
		end := min(start+playerNewsBatch, len(externalIDs))
		batchItems, err := fetchPlayerNews(ctx, client, externalIDs[start:end], playerIDs)
		if err != nil {
			return nil, err
		}
		items = append(items, batchItems...)
	}
	return items, nil
}

func espnPlayerIDs(players []news.Player) (map[string]string, []string) {
	playerIDs := make(map[string]string)
	var externalIDs []string
	for _, player := range players {
		externalID := player.ExternalIDs["espn"]
		if externalID == "" {
			continue
		}
		if _, exists := playerIDs[externalID]; exists {
			continue
		}
		playerIDs[externalID] = player.ID
		externalIDs = append(externalIDs, externalID)
	}
	return playerIDs, externalIDs
}

func fetchPlayerNews(ctx context.Context, client *http.Client, externalIDs []string, playerIDs map[string]string) ([]news.Item, error) {
	query := url.Values{}
	for _, externalID := range externalIDs {
		query.Add("playerId", externalID)
	}
	query.Set("limit", strconv.Itoa(playerNewsLimit))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, playerNewsURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("construct ESPN player news request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ESPN player news request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN player news returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, playerNewsMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read ESPN player news response: %w", err)
	}
	if len(body) > playerNewsMaxBody {
		return nil, fmt.Errorf("ESPN player news response exceeds size limit")
	}
	return parsePlayerNews(body, playerIDs)
}

// parsePlayerNews filters the mixed ESPN feed and maps its provider IDs to
// League Lens players without performing network or storage operations.
func parsePlayerNews(body []byte, playerIDs map[string]string) ([]news.Item, error) {
	var response playerNewsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid ESPN player news response: %w", err)
	}

	var items []news.Item
	for _, record := range response.Feed {
		if record.Type != "Rotowire" || record.Premium || record.ID == 0 || record.PlayerID == 0 || record.Headline == "" {
			continue
		}
		externalID := strconv.FormatInt(record.PlayerID, 10)
		playerID := playerIDs[externalID]
		if playerID == "" {
			continue
		}
		publishedAt, err := time.Parse(time.RFC3339, record.Published)
		if err != nil {
			continue
		}
		items = append(items, news.Item{
			PlayerID:     playerID,
			SourceItemID: strconv.FormatInt(record.ID, 10),
			Headline:     record.Headline,
			Summary:      record.Story,
			URL:          storyURL(record.ID, externalID),
			SourceLabel:  "RotoWire via ESPN",
			PublishedAt:  publishedAt,
		})
	}
	return items, nil
}

// storyURL links a RotoWire blurb to its page on rotowire.com. ESPN's item ID is
// the RotoWire news ID followed by "28"; other IDs use the ESPN player page.
func storyURL(itemID int64, espnPlayerID string) string {
	if itemID%100 == 28 && itemID >= 100 {
		return "https://www.rotowire.com/football/headlines/news-" + strconv.FormatInt(itemID/100, 10)
	}
	return "https://www.espn.com/nfl/player/_/id/" + espnPlayerID
}
