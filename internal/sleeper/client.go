// Package sleeper reads the public Sleeper API. It does not write to Sleeper.
package sleeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

type User struct {
	ID          string            `json:"user_id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata"`
}
type League struct {
	ID           string                     `json:"league_id"`
	Name         string                     `json:"name"`
	Season       string                     `json:"season"`
	Sport        string                     `json:"sport"`
	SeasonType   string                     `json:"season_type"`
	Status       string                     `json:"status"`
	TotalRosters int                        `json:"total_rosters"`
	Positions    []string                   `json:"roster_positions"`
	Settings     map[string]json.RawMessage `json:"settings"`
	Scoring      map[string]json.RawMessage `json:"scoring_settings"`
}
type Roster struct {
	ID       int                        `json:"roster_id"`
	OwnerID  string                     `json:"owner_id"`
	CoOwners []string                   `json:"co_owners"`
	Players  []string                   `json:"players"`
	Starters []string                   `json:"starters"`
	Reserve  []string                   `json:"reserve"`
	Taxi     []string                   `json:"taxi"`
	Settings map[string]json.RawMessage `json:"settings"`
}
type Matchup struct {
	RosterID      int                `json:"roster_id"`
	MatchupID     *int               `json:"matchup_id"`
	Players       []string           `json:"players"`
	Starters      []string           `json:"starters"`
	PlayersPoints map[string]float64 `json:"players_points"`
	Points        *float64           `json:"points"`
	CustomPoints  *float64           `json:"custom_points"`
}

// Projection is one player's projected stat line for a week from Sleeper's
// undocumented /projections endpoint. Stats are raw counts, not points.
type Projection struct {
	PlayerID string             `json:"player_id"`
	Stats    map[string]float64 `json:"stats"`
	Player   *projectionPlayer  `json:"player"`
}
type projectionPlayer struct {
	Name         string `json:"full_name"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Position     string `json:"position"`
	Team         string `json:"team"`
	InjuryStatus string `json:"injury_status"`
}
type State struct {
	Season     string `json:"season"`
	SeasonType string `json:"season_type"`
	Week       int    `json:"week"`
}
type Header struct {
	User   User
	League League
}
type Data struct {
	Header
	Rosters []Roster
	Users   []User
	Weeks   map[int][]Matchup
	// Projections holds each synced week's projections. A missing week means
	// its request failed and that week's lineups get no projections.
	Projections map[int][]Projection
	Period      int
	ObservedAt  time.Time
	State       State
}
type Client struct {
	HTTP    *http.Client
	BaseURL string
	// ProjectionsURL is the projections endpoint root, which sits outside /v1.
	ProjectionsURL string
	// StatsURL is the public weekly stats endpoint root, which sits outside /v1.
	StatsURL string
	// AvailabilityTeamIDs lists roster IDs whose observed players get injury
	// status and NFL team through per-player lookups. Empty disables lookups.
	AvailabilityTeamIDs []string
	PlayerCatalog       playerids.Catalog
	CatalogPath         string
}

func NewClient(h *http.Client) *Client {
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		HTTP:           h,
		BaseURL:        "https://api.sleeper.app/v1",
		ProjectionsURL: "https://api.sleeper.app/projections/nfl",
		StatsURL:       "https://api.sleeper.app/stats/nfl",
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.getURL(ctx, c.BaseURL+path, path, out)
}

// getURL reads rawURL into out, naming it by path in errors.
func (c *Client) getURL(ctx context.Context, rawURL, path string, out any) error {
	return c.getURLWithNull(ctx, rawURL, path, out, false)
}

func (c *Client) getURLAllowNull(ctx context.Context, rawURL, path string, out any) error {
	return c.getURLWithNull(ctx, rawURL, path, out, true)
}

func (c *Client) getURLWithNull(ctx context.Context, rawURL, path string, out any, allowNull bool) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Sleeper request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Sleeper %s returned HTTP %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if !allowNull && bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return fmt.Errorf("Sleeper %s returned null", path)
	}
	if err = json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("invalid Sleeper response for %s: %w", path, err)
	}
	return nil
}

func (c *Client) Header(ctx context.Context, username, leagueID string) (Header, error) {
	var h Header
	if username == "" || leagueID == "" {
		return h, fmt.Errorf("username and league ID required")
	}
	if err := c.get(ctx, "/user/"+url.PathEscape(username), &h.User); err != nil {
		return h, err
	}
	if h.User.ID == "" {
		return h, fmt.Errorf("Sleeper user not found")
	}
	if err := c.get(ctx, "/league/"+url.PathEscape(leagueID), &h.League); err != nil {
		return h, err
	}
	year, err := strconv.Atoi(h.League.Season)
	if err != nil ||
		year < 1900 || year > 2200 ||
		h.League.ID != leagueID ||
		h.League.Sport != "nfl" ||
		h.League.SeasonType != "regular" ||
		h.League.TotalRosters < 1 || h.League.TotalRosters > 100 {
		return h, fmt.Errorf("invalid or unsupported NFL league")
	}
	return h, nil
}

func (c *Client) Fetch(ctx context.Context, header Header, request fantasy.FetchRequest) (Data, error) {
	data := Data{Header: header, Weeks: map[int][]Matchup{}, ObservedAt: time.Now().UTC()}
	base := "/league/" + url.PathEscape(header.League.ID)
	if err := c.get(ctx, "/state/nfl", &data.State); err != nil {
		return data, err
	}
	period, err := importPeriod(data.State, header.League.Season)
	data.Period = period
	if err != nil {
		return data, err
	}
	if err = c.get(ctx, base+"/rosters", &data.Rosters); err != nil {
		return data, err
	}
	rosterIDs, err := validateLeagueRosters(data.Rosters, header)
	if err != nil {
		return data, err
	}
	if err = c.get(ctx, base+"/users", &data.Users); err != nil {
		return data, err
	}
	if data.State.SeasonType != "pre" {
		weeks, err := c.fetchMatchupWeeks(ctx, base, period, rosterIDs, request)
		// Retain fetched weeks in partial results, matching the existing error contract.
		data.Weeks = weeks
		if err != nil {
			return data, err
		}
		data.Projections = c.fetchProjections(ctx, header.League.Season, period, request)
	}
	return data, nil
}

// projectionPositions limits the weekly projections to fantasy positions,
// which keeps each response near 2 MB instead of 6 MB.
var projectionPositions = []string{"QB", "RB", "WR", "TE", "K", "DEF"}

// fetchProjections reads the league season's projections for every week the
// request doesn't settle. The endpoint is undocumented, so a failed week is
// logged and skipped rather than failing the import.
func (c *Client) fetchProjections(ctx context.Context, season string, period int, request fantasy.FetchRequest) map[int][]Projection {
	query := url.Values{"season_type": {"regular"}, "position[]": projectionPositions}.Encode()
	projections := map[int][]Projection{}
	for week := 1; week <= period; week++ {
		if request.SkipsWeek(fantasy.ScoringPeriod(week), fantasy.ScoringPeriod(period)) {
			continue
		}
		path := "/" + url.PathEscape(season) + "/" + strconv.Itoa(week)
		var rows []Projection
		if err := c.getURL(ctx, c.ProjectionsURL+path+"?"+query, "/projections/nfl"+path, &rows); err != nil {
			slog.Warn("sleeper projections fetch failed; week has no projections", "season", season, "week", week, "error", err)
			continue
		}
		projections[week] = rows
	}
	return projections
}

// fetchMatchupWeeks reads each week once, sequentially, except weeks the
// request settles: at most 18 small requests per import.
func (c *Client) fetchMatchupWeeks(
	ctx context.Context,
	base string,
	period int,
	rosterIDs map[int]bool,
	request fantasy.FetchRequest,
) (map[int][]Matchup, error) {
	weeks := map[int][]Matchup{}
	for week := 1; week <= period; week++ {
		if request.SkipsWeek(fantasy.ScoringPeriod(week), fantasy.ScoringPeriod(period)) {
			continue
		}
		var matchups []Matchup
		if err := c.get(ctx, base+"/matchups/"+strconv.Itoa(week), &matchups); err != nil {
			return weeks, err
		}
		if err := validateWeeklyRosters(matchups, rosterIDs, week); err != nil {
			return weeks, err
		}
		weeks[week] = matchups
	}
	return weeks, nil
}
