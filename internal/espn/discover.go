package espn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

const fanAPIBaseURL = "https://fan.api.espn.com/apis/v2/fans/"

type fanResponse struct {
	Preferences []struct {
		MetaData struct {
			Entry struct {
				EntryID  int    `json:"entryId"`
				Name     string `json:"name"`
				GameID   int    `json:"gameId"`
				SeasonID int    `json:"seasonId"`
				Groups   []struct {
					GroupID   int    `json:"groupId"`
					GroupName string `json:"groupName"`
				} `json:"groups"`
			} `json:"entry"`
		} `json:"metaData"`
	} `json:"preferences"`
}

// DiscoverLeagues validates the cookies and returns the account's ESPN football leagues and teams.
func DiscoverLeagues(ctx context.Context, httpClient *http.Client, creds Credentials) ([]fantasy.DiscoveredLeague, error) {
	credentials := creds
	credentials.SWID = NormalizeSWID(credentials.SWID)
	if credentials.S2 == "" || credentials.SWID == "" {
		return nil, fmt.Errorf("ESPN_S2 and ESPN_SWID are required")
	}

	endpoint, err := url.Parse(fanAPIBaseURL + url.PathEscape(credentials.SWID))
	if err != nil {
		return nil, fmt.Errorf("cannot construct ESPN fan request")
	}
	endpoint.RawQuery = url.Values{
		"displayHiddenPrefs": {"true"},
		"source":             {"espncom-fantasy-lm"},
		"lang":               {"en"},
		"section":            {"espn"},
		"region":             {"us"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot construct ESPN fan request")
	}
	request.AddCookie(&http.Cookie{Name: "espn_s2", Value: credentials.S2})
	request.AddCookie(&http.Cookie{Name: "SWID", Value: credentials.SWID})

	privateClient := privateHTTPClient(httpClient)
	response, err := privateClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ESPN fan request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: ESPN fan API returned HTTP %d", ErrUnauthorized, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN fan API returned HTTP %d", response.StatusCode)
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit {
		return nil, fmt.Errorf("cannot read ESPN fan response")
	}
	var parsed fanResponse
	if json.Unmarshal(body, &parsed) != nil {
		return nil, fmt.Errorf("invalid ESPN fan response")
	}
	leagues := discoveredFootballLeagues(parsed)
	if len(leagues) > 0 {
		firstLeague := leagues[0].Ref
		if _, err := NewClient(httpClient, credentials, firstLeague.Season).DescribeLeague(ctx, firstLeague.ExternalID); err != nil {
			if errors.Is(err, ErrUnauthorized) {
				return nil, err
			}
			return nil, fmt.Errorf("ESPN league validation failed")
		}
	}
	return leagues, nil
}

func discoveredFootballLeagues(response fanResponse) []fantasy.DiscoveredLeague {
	var leagues []fantasy.DiscoveredLeague
	seen := make(map[string]bool)
	for _, preference := range response.Preferences {
		entry := preference.MetaData.Entry
		if entry.GameID != 1 || entry.EntryID <= 0 || entry.SeasonID < 1900 || entry.SeasonID > 2200 {
			continue
		}
		for _, group := range entry.Groups {
			if group.GroupID <= 0 {
				continue
			}
			leagueID := strconv.Itoa(group.GroupID)
			teamID := strconv.Itoa(entry.EntryID)
			key := leagueID + ":" + strconv.Itoa(entry.SeasonID) + ":" + teamID
			if seen[key] {
				continue
			}
			seen[key] = true
			leagues = append(leagues, fantasy.DiscoveredLeague{
				Ref:      fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: leagueID, Season: entry.SeasonID},
				Name:     strings.TrimSpace(group.GroupName),
				TeamID:   teamID,
				TeamName: strings.TrimSpace(entry.Name),
			})
		}
	}
	return leagues
}
