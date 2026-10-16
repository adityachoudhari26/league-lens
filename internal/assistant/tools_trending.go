package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const getTrendingPlayersSchema = `{
  "type": "object",
  "properties": {
    "type": {"type": "string", "enum": ["add", "drop"], "default": "add"},
    "lookback_hours": {"type": "integer", "minimum": 1, "maximum": 168, "default": 24},
    "limit": {"type": "integer", "minimum": 1, "maximum": 25, "default": 15},
    "league_season_id": {"type": "string", "format": "uuid"}
  },
  "additionalProperties": false
}`

const (
	trendingFetchLimit = 50
	trendingCacheTTL   = 15 * time.Minute
)

// TrendingSource reads Sleeper's public ranked add/drop lists.
type TrendingSource interface {
	TrendingPlayers(context.Context, string, int, int) ([]sleeper.TrendingPlayer, error)
}

type trendingToolInput struct {
	Type           string `json:"type"`
	LookbackHours  *int   `json:"lookback_hours"`
	Limit          *int   `json:"limit"`
	LeagueSeasonID string `json:"league_season_id"`
}

type trendingQuery struct {
	kind          string
	lookbackHours int
	limit         int
	leagueSeason  pgtype.UUID
	hasLeague     bool
}

type trendingToolResult struct {
	Type          string           `json:"type"`
	LookbackHours int              `json:"lookback_hours"`
	Players       []trendingPlayer `json:"players"`
}

type trendingPlayer struct {
	Name         string `json:"name"`
	Position     string `json:"position"`
	NFLTeam      string `json:"nfl_team"`
	Count        int    `json:"count"`
	Availability string `json:"availability,omitempty"`
	sleeperID    string
	providerIDs  map[string]string
}

type trendingCacheKey struct {
	kind          string
	lookbackHours int
}

type trendingCacheEntry struct {
	players []sleeper.TrendingPlayer
	expires time.Time
}

type trendingCache struct {
	mu      sync.Mutex
	entries map[trendingCacheKey]trendingCacheEntry
}

func trendingTools(service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_trending_players",
		"Players other fantasy managers are adding or dropping most across Sleeper right now, "+
			"most-added (or most-dropped) first. Use it for waiver and pickup questions; "+
			"pass league_season_id to also see whether each player is available, on your team, or "+
			"rostered by another team in that league.",
		getTrendingPlayersSchema,
		fixedToolLabel("Checking trending players"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[trendingToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			query, err := validateTrendingInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			result, err := service.trendingPlayers(ctx, query)
			if err != nil {
				return "", err
			}
			content, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			return string(content), nil
		},
	)}
}

func validateTrendingInput(input trendingToolInput) (trendingQuery, error) {
	query := trendingQuery{kind: input.Type, lookbackHours: 24, limit: 15}
	if query.kind == "" {
		query.kind = "add"
	}
	if input.LookbackHours != nil {
		query.lookbackHours = *input.LookbackHours
	}
	if input.Limit != nil {
		query.limit = *input.Limit
	}
	if (query.kind != "add" && query.kind != "drop") ||
		query.lookbackHours < 1 || query.lookbackHours > 168 ||
		query.limit < 1 || query.limit > 25 {
		return trendingQuery{}, errInvalidToolInput
	}
	if input.LeagueSeasonID != "" {
		if err := query.leagueSeason.Scan(input.LeagueSeasonID); err != nil {
			return trendingQuery{}, errInvalidToolInput
		}
		query.hasLeague = true
	}
	return query, nil
}

// trendingPlayers maps Sleeper's ranked IDs through the shared catalog and optionally adds private league availability.
func (s *Service) trendingPlayers(ctx context.Context, query trendingQuery) (trendingToolResult, error) {
	rows, err := s.cachedTrendingPlayers(ctx, query.kind, query.lookbackHours)
	if err != nil {
		return trendingToolResult{}, err
	}
	catalog, err := s.trendingCatalog(ctx)
	if err != nil {
		return trendingToolResult{}, errors.New("player catalog could not be loaded")
	}
	players := mapTrendingPlayers(rows, catalog, query.limit)
	if query.hasLeague {
		if err := s.addTrendingAvailability(ctx, query.leagueSeason, players); err != nil {
			return trendingToolResult{}, err
		}
	}
	return trendingToolResult{
		Type: query.kind, LookbackHours: query.lookbackHours, Players: players,
	}, nil
}

func (s *Service) cachedTrendingPlayers(ctx context.Context, kind string, lookbackHours int) ([]sleeper.TrendingPlayer, error) {
	key := trendingCacheKey{kind: kind, lookbackHours: lookbackHours}
	s.trending.mu.Lock()
	defer s.trending.mu.Unlock()
	if s.trending.entries == nil {
		s.trending.entries = make(map[trendingCacheKey]trendingCacheEntry)
	}
	if entry, found := s.trending.entries[key]; found && time.Now().Before(entry.expires) {
		return append([]sleeper.TrendingPlayer(nil), entry.players...), nil
	}
	players, err := s.Trending.TrendingPlayers(ctx, kind, lookbackHours, trendingFetchLimit)
	if err != nil {
		return nil, err
	}
	s.trending.entries[key] = trendingCacheEntry{
		players: append([]sleeper.TrendingPlayer(nil), players...),
		expires: time.Now().Add(trendingCacheTTL),
	}
	return players, nil
}

func (s *Service) trendingCatalog(ctx context.Context) (playerids.Catalog, error) {
	if s.PlayerCatalog != nil {
		return s.PlayerCatalog, nil
	}
	if s.CatalogPath == "" {
		return nil, errors.New("player catalog path is not configured")
	}
	catalog, _, err := playerids.Load(ctx, s.CatalogPath)
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

func mapTrendingPlayers(rows []sleeper.TrendingPlayer, catalog playerids.Catalog, limit int) []trendingPlayer {
	players := make([]trendingPlayer, 0, limit)
	for _, row := range rows {
		entry, found := catalog.Lookup("sleeper", row.PlayerID)
		if !found {
			continue
		}
		players = append(players, trendingPlayer{
			Name: entry.Name, Position: trendingPosition(entry.Position), NFLTeam: entry.Team,
			Count: row.Count, sleeperID: row.PlayerID, providerIDs: entry.IDs,
		})
		if len(players) == limit {
			break
		}
	}
	return players
}

func trendingPosition(position string) string {
	switch position {
	case "PK":
		return "K"
	case "DEF":
		return "DST"
	default:
		return position
	}
}

func leagueTrendingIDs(provider string, players []trendingPlayer) ([]string, map[string][]int) {
	var externalIDs []string
	playerIndexesByExternalID := make(map[string][]int, len(players))
	for index, player := range players {
		var externalID string
		switch provider {
		case "sleeper":
			externalID = player.sleeperID
		case "espn":
			externalID = player.providerIDs["espn"]
		}
		if externalID == "" {
			continue
		}
		if _, found := playerIndexesByExternalID[externalID]; !found {
			externalIDs = append(externalIDs, externalID)
		}
		playerIndexesByExternalID[externalID] = append(playerIndexesByExternalID[externalID], index)
	}
	return externalIDs, playerIndexesByExternalID
}

func trendingAvailability(
	row store.AssistantTrendingRosteredPlayersRow,
	rostered bool,
	selectedTeams []pgtype.UUID,
) string {
	if !rostered {
		return "available"
	}
	if slices.Contains(selectedTeams, row.TeamID) {
		return "on your team"
	}
	return "rostered by " + row.TeamName
}

// addTrendingAvailability uses each team's latest succeeded observed snapshot, leaving unmapped players without availability.
func (s *Service) addTrendingAvailability(
	ctx context.Context,
	leagueID pgtype.UUID,
	players []trendingPlayer,
) error {
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		league, err := q.DashboardLeague(ctx, store.DashboardLeagueParams{
			UserID: owner.ID, ID: leagueID,
		})
		if err != nil {
			return err
		}
		externalIDs, playerIndexesByExternalID := leagueTrendingIDs(league.Provider, players)
		if len(externalIDs) == 0 {
			return nil
		}
		rows, err := q.AssistantTrendingRosteredPlayers(ctx, store.AssistantTrendingRosteredPlayersParams{
			LeagueSeasonID: leagueID, ExternalIds: externalIDs,
		})
		if err != nil {
			return err
		}
		rostered := make(map[string]store.AssistantTrendingRosteredPlayersRow, len(externalIDs))
		for _, row := range rows {
			rostered[row.ExternalID] = row
		}
		for externalID, indexes := range playerIndexesByExternalID {
			row, found := rostered[externalID]
			for _, index := range indexes {
				players[index].Availability = trendingAvailability(row, found, league.SelectedTeamIds)
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return connect.NewError(connect.CodeNotFound, "league not found")
	}
	if err != nil {
		return internalError("trending availability could not be loaded")
	}
	return nil
}
