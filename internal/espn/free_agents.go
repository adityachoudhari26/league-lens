package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.FreeAgentLister = (*Client)(nil)

type freeAgentFilter struct {
	Players map[string]any `json:"players"`
}

type freeAgentResponse struct {
	Players []freeAgentEntry `json:"players"`
}

type freeAgentEntry struct {
	Status string          `json:"status"`
	Player freeAgentPlayer `json:"player"`
}

type freeAgentPlayer struct {
	ID           int    `json:"id"`
	Name         string `json:"fullName"`
	Position     int    `json:"defaultPositionId"`
	ProTeam      int    `json:"proTeamId"`
	InjuryStatus string `json:"injuryStatus"`
	Ownership    struct {
		PercentOwned *float64 `json:"percentOwned"`
	} `json:"ownership"`
	Stats []statResponse `json:"stats"`
}

// ListFreeAgents reads the league's ESPN player pool and current-week scoring.
func (c *Client) ListFreeAgents(ctx context.Context, league fantasy.League, query fantasy.FreeAgentQuery) ([]fantasy.FreeAgent, error) {
	leagueID, parseErr := strconv.ParseUint(league.Ref.ExternalID, 10, 64)
	if league.Ref.Provider != fantasy.ProviderESPN || parseErr != nil || leagueID == 0 ||
		league.Ref.Season < 1900 || league.Ref.Season > 2200 ||
		query.Week < 1 || query.Week > 18 {
		return nil, fmt.Errorf("valid ESPN league and week required")
	}
	if c.credentials.S2 == "" || c.credentials.SWID == "" {
		return nil, fmt.Errorf("ESPN credentials required")
	}
	if league.Ref.Season != c.season {
		return nil, fmt.Errorf("ESPN season mismatch")
	}
	filter := freeAgentFilter{Players: map[string]any{
		"filterStatus": map[string]any{"value": []string{"FREEAGENT", "WAIVERS"}},
		"limit":        25,
		"sortPercOwned": map[string]any{
			"sortPriority": 1,
			"sortAsc":      false,
		},
		// Live-probed: this returns weekly projections; map by the requested scoring period below.
		"filterStatsForSplitTypeIds": map[string]any{"value": []int{0, 1}},
	}}
	if query.Position != "" {
		slot, ok := espnFreeAgentSlot(query.Position)
		if !ok {
			return nil, fmt.Errorf("unsupported ESPN fantasy position")
		}
		filter.Players["filterSlotIds"] = map[string]any{"value": []int{slot}}
	}
	players, err := c.readFreeAgentPool(ctx, league.Ref.ExternalID, filter)
	if err != nil {
		return nil, err
	}
	agents := make([]fantasy.FreeAgent, 0, len(players))
	for _, entry := range players {
		player, status := entry.Player, entry.Status
		if player.ID <= 0 || status != "FREEAGENT" && status != "WAIVERS" {
			continue
		}
		agent := fantasy.FreeAgent{
			ExternalPlayerID: strconv.Itoa(player.ID),
			Name:             player.Name,
			Position:         espnFreeAgentPosition(player.Position),
			NFLTeam:          proTeamAbbreviation(player.ProTeam),
			InjuryStatus:     player.InjuryStatus,
			Status:           strings.ToLower(status),
			PercentRostered:  player.Ownership.PercentOwned,
		}
		for _, stat := range player.Stats {
			if stat.SourceID == 0 && stat.SplitTypeID == 0 &&
				stat.SeasonID == league.Ref.Season && stat.AppliedTotal != nil {
				agent.SeasonPoints = stat.AppliedTotal
			}
			if stat.SourceID == 1 && stat.SplitTypeID == 1 &&
				stat.SeasonID == league.Ref.Season && stat.ScoringPeriodID == query.Week && stat.AppliedTotal != nil {
				agent.ProjectedPoints = stat.AppliedTotal
			}
		}
		agents = append(agents, agent)
	}
	return fantasy.SortFreeAgents(agents, query.Limit), nil
}

func (c *Client) readFreeAgentPool(ctx context.Context, leagueID string, filter freeAgentFilter) ([]freeAgentEntry, error) {
	encodedFilter, err := json.Marshal(filter)
	if err != nil {
		return nil, fmt.Errorf("cannot encode ESPN player filter")
	}
	query := url.Values{"view": {"kona_player_info"}}
	endpoint := fmt.Sprintf("%s/seasons/%d/segments/0/leagues/%s?%s", c.baseURL, c.season, leagueID, query.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot construct ESPN player request")
	}
	request.AddCookie(&http.Cookie{Name: "espn_s2", Value: c.credentials.S2})
	request.AddCookie(&http.Cookie{Name: "SWID", Value: c.credentials.SWID})
	request.Header.Set("x-fantasy-filter", string(encodedFilter))
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ESPN player request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("%w: ESPN returned HTTP %d", ErrUnauthorized, response.StatusCode)
		}
		return nil, fmt.Errorf("ESPN returned HTTP %d", response.StatusCode)
	}
	const maxPlayerPoolBytes = 16 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPlayerPoolBytes+1))
	if err != nil || len(body) > maxPlayerPoolBytes {
		return nil, fmt.Errorf("cannot read ESPN player response")
	}
	var result freeAgentResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid ESPN player response")
	}
	return result.Players, nil
}

func espnFreeAgentSlot(position string) (int, bool) {
	switch strings.ToUpper(position) {
	case "QB":
		return 0, true
	case "RB":
		return 2, true
	case "WR":
		return 4, true
	case "TE":
		return 6, true
	case "DST":
		return 16, true
	case "K":
		return 17, true
	default:
		return 0, false
	}
}

func espnFreeAgentPosition(id int) string {
	switch id {
	case 1:
		return "QB"
	case 2:
		return "RB"
	case 3:
		return "WR"
	case 4:
		return "TE"
	case 5:
		return "K"
	case 16:
		return "DST"
	default:
		return ""
	}
}
