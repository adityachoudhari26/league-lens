// Package espn adapts ESPN's undocumented fantasy football API.
package espn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Credentials struct{ S2, SWID string }

var ErrUnauthorized = errors.New("ESPN rejected credentials")

type Client struct {
	http        *http.Client
	credentials Credentials
	season      int
	baseURL     string
}

func NewClient(client *http.Client, credentials Credentials, season int) *Client {
	privateClient := privateHTTPClient(client)
	credentials.SWID = NormalizeSWID(credentials.SWID)
	return &Client{http: &privateClient, credentials: credentials, season: season, baseURL: "https://lm-api-reads.fantasy.espn.com/apis/v3/games/ffl"}
}

func privateHTTPClient(client *http.Client) http.Client {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	privateClient := *client
	privateClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return privateClient
}

// NormalizeSWID adds the braces required by ESPN's account endpoints.
func NormalizeSWID(swid string) string {
	swid = strings.TrimSpace(swid)
	if swid == "" || strings.HasPrefix(swid, "{") {
		return swid
	}
	return "{" + swid + "}"
}

// AccountID identifies the local connection without storing the SWID cookie.
func (c *Client) AccountID() string {
	return fmt.Sprintf("espn-account:%x", sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(c.credentials.SWID)))))
}

func (c *Client) readLeague(ctx context.Context, id string, week int) (leagueResponse, error) {
	return c.readLeagueWithViews(ctx, id, week, []string{"mSettings", "mTeam", "mRoster", "mMatchup", "mMatchupScore", "mBoxscore"}, nil)
}

// readLeagueWithViews fetches one league week using the requested views and headers.
func (c *Client) readLeagueWithViews(
	ctx context.Context,
	id string,
	week int,
	views []string,
	headers http.Header,
) (leagueResponse, error) {
	var result leagueResponse
	leagueID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || leagueID == 0 || c.season < 1900 || c.season > 2200 {
		return result, fmt.Errorf("valid ESPN league ID and season required")
	}
	if c.credentials.S2 == "" || c.credentials.SWID == "" {
		return result, fmt.Errorf("ESPN_S2 and ESPN_SWID are required")
	}
	query := url.Values{}
	for _, view := range views {
		query.Add("view", view)
	}
	if week > 0 {
		query.Set("scoringPeriodId", strconv.Itoa(week))
	}
	endpoint := fmt.Sprintf("%s/seasons/%d/segments/0/leagues/%s?%s", c.baseURL, c.season, id, query.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result, fmt.Errorf("cannot construct ESPN request")
	}
	request.AddCookie(&http.Cookie{Name: "espn_s2", Value: c.credentials.S2})
	request.AddCookie(&http.Cookie{Name: "SWID", Value: c.credentials.SWID})
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	if err := c.fetchJSON(request, &result); err != nil {
		return result, err
	}
	if result.ID != int64(leagueID) || result.Season != c.season {
		return result, fmt.Errorf("ESPN league identity mismatch")
	}
	if week > 0 && result.Week != week {
		return result, fmt.Errorf("ESPN did not return requested historical week %d", week)
	}
	return result, nil
}

func (c *Client) fetchJSON(request *http.Request, result *leagueResponse) error {
	response, err := c.http.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return request.Context().Err()
		}
		return fmt.Errorf("ESPN request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: ESPN returned HTTP %d; check league access and credentials", ErrUnauthorized, response.StatusCode)
		}
		return fmt.Errorf("ESPN returned HTTP %d; check league access and credentials", response.StatusCode)
	}
	const limit = 16 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit {
		return fmt.Errorf("cannot read ESPN response")
	}
	if json.Unmarshal(body, result) != nil {
		return fmt.Errorf("invalid ESPN response")
	}
	return nil
}
