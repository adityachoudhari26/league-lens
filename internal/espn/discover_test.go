package espn

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

type discoverRoundTripper func(*http.Request) (*http.Response, error)

func (f discoverRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDiscoverLeaguesUsesFootballPreferences(t *testing.T) {
	client := &http.Client{Transport: discoverRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "lm-api-reads.fantasy.espn.com" {
			if cookie, err := request.Cookie("espn_s2"); err != nil || cookie.Value != "synthetic-s2" {
				t.Error("missing ESPN S2 cookie on league validation")
			}
			body := `{"id":34,"seasonId":2026,"settings":{"name":"Football League","size":2},"teams":[{"id":12},{"id":13}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if request.URL.Host != "fan.api.espn.com" || request.URL.Path != "/apis/v2/fans/{synthetic-swid}" {
			t.Errorf("unexpected request target: %s", request.URL.Redacted())
		}
		want := url.Values{
			"displayHiddenPrefs": {"true"},
			"source":             {"espncom-fantasy-lm"},
			"lang":               {"en"},
			"section":            {"espn"},
			"region":             {"us"},
		}
		if request.URL.Query().Encode() != want.Encode() {
			t.Errorf("unexpected query parameters: %v", request.URL.Query())
		}
		if cookie, err := request.Cookie("SWID"); err != nil || cookie.Value != "{synthetic-swid}" {
			t.Error("missing normalized SWID cookie")
		}
		if cookie, err := request.Cookie("espn_s2"); err != nil || cookie.Value != "synthetic-s2" {
			t.Error("missing ESPN S2 cookie")
		}
		s2Cookies := 0
		for _, cookie := range request.Cookies() {
			if cookie.Name == "espn_s2" {
				s2Cookies++
			}
		}
		if s2Cookies != 1 {
			t.Errorf("ESPN S2 cookie count = %d, want 1", s2Cookies)
		}
		body := `{"preferences":[
			{"metaData":{"entry":{
				"entryId":12,
				"name":"Fantasy Football 2026",
				"entryMetadata":{"teamName":"My Team","teamAbbrev":"MT"},
				"gameId":1,
				"seasonId":2026,
				"groups":[{"groupId":34,"groupName":"Football League"}]
			}}},
			{"metaData":{"entry":{"entryId":99,"name":"Other Sport","gameId":2,"seasonId":2026,"groups":[{"groupId":56,"groupName":"Not Football"}]}}},
			{"metaData":{"entry":{"entryId":13,"name":"Other Team","gameId":1,"seasonId":2026,"groups":[{"groupId":34,"groupName":"Football League"}]}}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	leagues, err := DiscoverLeagues(context.Background(), client, Credentials{S2: "synthetic-s2", SWID: "synthetic-swid"})
	if err != nil {
		t.Fatal(err)
	}
	want := []fantasy.DiscoveredLeague{
		{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "34", Season: 2026}, Name: "Football League", TeamID: "12", TeamName: "My Team"},
		{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "34", Season: 2026}, Name: "Football League", TeamID: "13", TeamName: "Other Team"},
	}
	if len(leagues) != len(want) {
		t.Fatalf("got %d leagues, want %d", len(leagues), len(want))
	}
	for index := range want {
		if leagues[index] != want[index] {
			t.Fatalf("league %d = %+v, want %+v", index, leagues[index], want[index])
		}
	}
}

func TestDiscoverLeaguesRejectsUnauthorizedLeagueRead(t *testing.T) {
	client := &http.Client{Transport: discoverRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "fan.api.espn.com" {
			body := `{
				"preferences": [{
					"metaData": {
						"entry": {
							"entryId": 12,
							"name": "My Team",
							"gameId": 1,
							"seasonId": 2026,
							"groups": [{"groupId": 34, "groupName": "Football League"}]
						}
					}
				}]
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}

	_, err := DiscoverLeagues(context.Background(), client, Credentials{S2: "synthetic-s2", SWID: "{synthetic-swid}"})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error %v does not wrap ErrUnauthorized", err)
	}
}

func TestDiscoverLeaguesWrapsUnauthorizedStatuses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: discoverRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}
			_, err := DiscoverLeagues(context.Background(), client, Credentials{S2: "synthetic-s2", SWID: "{synthetic-swid}"})
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("error %v does not wrap ErrUnauthorized", err)
			}
		})
	}
}
