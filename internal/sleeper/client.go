// Package sleeper reads the public Sleeper API. It does not write to Sleeper.
package sleeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
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
	RosterID     int      `json:"roster_id"`
	MatchupID    *int     `json:"matchup_id"`
	Players      []string `json:"players"`
	Starters     []string `json:"starters"`
	Points       *float64 `json:"points"`
	CustomPoints *float64 `json:"custom_points"`
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
	Rosters    []Roster
	Users      []User
	Weeks      map[int][]Matchup
	Period     int
	ObservedAt time.Time
	State      State
}
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

func NewClient(h *http.Client) *Client {
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{HTTP: h, BaseURL: "https://api.sleeper.app/v1"}
}
func (c *Client) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
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
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
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
	if err != nil || year < 1900 || year > 2200 || h.League.ID != leagueID || h.League.Sport != "nfl" || h.League.SeasonType != "regular" || h.League.TotalRosters < 1 || h.League.TotalRosters > 100 {
		return h, fmt.Errorf("invalid or unsupported NFL league")
	}
	return h, nil
}
func (c *Client) Fetch(ctx context.Context, h Header) (Data, error) {
	d := Data{Header: h, Weeks: map[int][]Matchup{}, ObservedAt: time.Now().UTC()}
	base := "/league/" + url.PathEscape(h.League.ID)
	if err := c.get(ctx, "/state/nfl", &d.State); err != nil {
		return d, err
	}
	if d.State.Season != h.League.Season {
		return d, fmt.Errorf("only the current NFL season is supported")
	}
	switch d.State.SeasonType {
	case "regular":
		d.Period = d.State.Week
	case "pre":
		d.Period = 1
	case "post":
		d.Period = 18
	default:
		return d, fmt.Errorf("unsupported NFL season state")
	}
	if d.Period < 1 || d.Period > 18 {
		return d, fmt.Errorf("invalid NFL week")
	}
	if err := c.get(ctx, base+"/rosters", &d.Rosters); err != nil {
		return d, err
	}
	if len(d.Rosters) != h.League.TotalRosters {
		return d, fmt.Errorf("incomplete league rosters")
	}
	ids := map[int]bool{}
	member := false
	for _, r := range d.Rosters {
		if r.ID <= 0 || ids[r.ID] {
			return d, fmt.Errorf("invalid or duplicate roster ID")
		}
		ids[r.ID] = true
		if r.OwnerID == h.User.ID {
			member = true
		}
		for _, id := range r.CoOwners {
			if id == h.User.ID {
				member = true
			}
		}
	}
	if !member {
		return d, fmt.Errorf("Sleeper user does not own a team in this league")
	}
	if err := c.get(ctx, base+"/users", &d.Users); err != nil {
		return d, err
	}
	// Fetch each week once, sequentially: bounded to 18 small requests per import.
	if d.State.SeasonType != "pre" {
		for week := 1; week <= d.Period; week++ {
			var matches []Matchup
			if err := c.get(ctx, base+"/matchups/"+strconv.Itoa(week), &matches); err != nil {
				return d, err
			}
			seen := map[int]bool{}
			for _, m := range matches {
				if !ids[m.RosterID] || seen[m.RosterID] {
					return d, fmt.Errorf("invalid weekly roster")
				}
				seen[m.RosterID] = true
			}
			// Empty weeks are unavailable history, never empty roster snapshots.
			if len(matches) > 0 && len(matches) != len(d.Rosters) {
				return d, fmt.Errorf("incomplete matchup week %d", week)
			}
			d.Weeks[week] = matches
		}
	}
	return d, nil
}
