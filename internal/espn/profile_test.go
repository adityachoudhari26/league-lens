package espn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/playerprofile"
)

const (
	bijanCorePath      = "/v2/sports/football/leagues/nfl/athletes/4430807"
	bijanAthletePath   = "/apis/common/v3/sports/football/nfl/athletes/4430807"
	bijanOverviewPath  = bijanAthletePath + "/overview"
	fantasyPlayersPath = "/apis/v3/games/ffl/seasons/2026/players"
)

func TestProfileAssemblesAthleteFromESPN(t *testing.T) {
	source := profileTestSource(t, map[string]string{
		bijanCorePath:      "testdata/profile_core_athlete.json",
		bijanAthletePath:   "testdata/profile_athlete.json",
		bijanOverviewPath:  "testdata/profile_athlete_overview.json",
		fantasyPlayersPath: "testdata/profile_fantasy_player.json",
	})

	profile, err := source.Profile(context.Background(), espnPlayer("4430807"))
	if err != nil {
		t.Fatal(err)
	}
	if want := wantBijanProfile(); !reflect.DeepEqual(profile, want) {
		t.Fatalf("profile =\n%+v\nwant\n%+v", profile, want)
	}
}

func TestProfileBuildsDefenseFromFantasyRecord(t *testing.T) {
	source := profileTestSource(t, map[string]string{fantasyPlayersPath: "testdata/profile_fantasy_defense.json"})

	profile, err := source.Profile(context.Background(), espnPlayer("-16012"))
	if err != nil {
		t.Fatal(err)
	}
	want := playerprofile.Profile{
		HeadshotURL:     "https://a.espncdn.com/i/teamlogos/nfl/500/kc.png",
		PercentRostered: ptr(71.88),
		PercentStarted:  ptr(57.27),
		SeasonOutlook: "The Chiefs have fielded a middling fantasy D/ST the past two seasons, scoring in double-digits only seven times  " +
			"but finishing with zero or negative fantasy points only five times .",
	}
	if !reflect.DeepEqual(profile, want) {
		t.Fatalf("profile = %+v, want %+v", profile, want)
	}
}

func TestProfileLeavesOptionalResponsesUnreported(t *testing.T) {
	source := profileTestSource(t, map[string]string{bijanCorePath: "testdata/profile_core_athlete.json"})

	profile, err := source.Profile(context.Background(), espnPlayer("4430807"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.HeadshotURL == "" || profile.College != "" || profile.PositionRank != nil || profile.SeasonStats != nil || profile.WeeklyOutlook != nil {
		t.Fatalf("profile = %+v; want bio only", profile)
	}
}

func TestProfileReportsUnknownPlayers(t *testing.T) {
	tests := []struct {
		name   string
		player playerprofile.Player
	}{
		{name: "no ESPN ID", player: playerprofile.Player{ID: "only-sleeper", ExternalIDs: map[string]string{"sleeper": "4046"}}},
		{name: "malformed ESPN ID", player: espnPlayer("abc")},
		{name: "athlete missing at ESPN", player: espnPlayer("4430807")},
		{name: "defense missing from fantasy pool", player: espnPlayer("-16012")},
	}
	source := profileTestSource(t, map[string]string{fantasyPlayersPath: "testdata/empty_array.json"})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := source.Profile(context.Background(), test.player); !errors.Is(err, playerprofile.ErrUnknownPlayer) {
				t.Fatalf("err = %v, want ErrUnknownPlayer", err)
			}
		})
	}
}

func TestProfileFailsOnESPNErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	source := ProfileSource{HTTP: redirectedClient(t, server), Season: 2026}

	_, err := source.Profile(context.Background(), espnPlayer("4430807"))
	if err == nil || errors.Is(err, playerprofile.ErrUnknownPlayer) {
		t.Fatalf("err = %v, want an ESPN failure", err)
	}
}

func TestLatestOutlookPicksNewestWrittenWeek(t *testing.T) {
	got := latestOutlook(map[string]string{"2": "Week 2.", "6": " Week 6. ", "7": "  ", "x": "Unnumbered."})
	if want := (&playerprofile.Outlook{Week: 6, Text: "Week 6."}); !reflect.DeepEqual(got, want) {
		t.Fatalf("latestOutlook = %+v, want %+v", got, want)
	}
	if got := latestOutlook(nil); got != nil {
		t.Fatalf("latestOutlook(nil) = %+v, want nil", got)
	}
}

// profileTestSource serves fixture files by request path; other paths 404 like
// ESPN does for unknown players. Fantasy requests must carry the player filter.
func profileTestSource(t *testing.T, fixtures map[string]string) ProfileSource {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fantasyPlayersPath {
			assertFantasyPlayerRequest(t, r)
		}
		fixture, ok := fixtures[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(fixture)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return ProfileSource{HTTP: redirectedClient(t, server), Season: 2026}
}

func assertFantasyPlayerRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.URL.Query().Get("view"); got != "kona_player_info" {
		t.Errorf("view = %q, want kona_player_info", got)
	}
	if got := r.Header.Get("X-Fantasy-Filter"); got != `{"filterIds":{"value":[4430807]}}` && got != `{"filterIds":{"value":[-16012]}}` {
		t.Errorf("X-Fantasy-Filter = %q", got)
	}
}

func redirectedClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: redirectTransport{target: serverURL}}
}

func espnPlayer(espnID string) playerprofile.Player {
	return playerprofile.Player{ID: "league-player", ExternalIDs: map[string]string{"espn": espnID}}
}

func wantBijanProfile() playerprofile.Profile {
	birthDate := time.Date(2002, 1, 30, 0, 0, 0, 0, time.UTC)
	return playerprofile.Profile{
		HeadshotURL:     "https://a.espncdn.com/i/headshots/nfl/players/full/4430807.png",
		Jersey:          "7",
		BirthDate:       &birthDate,
		HeightInches:    ptr(71),
		WeightLbs:       ptr(215),
		College:         "Texas",
		Draft:           &playerprofile.Draft{Year: 2023, Round: 1, Pick: 8},
		PositionRank:    ptr(3),
		PercentRostered: ptr(99.92),
		PercentStarted:  ptr(99.71),
		WeeklyOutlook: &playerprofile.Outlook{
			Week: 6,
			Text: "Bijan Robinson will lead Atlanta's offensive attack in the Week 6 matchup against the Bears.",
		},
		SeasonOutlook: "Robinson enters his fourth season having finished in the top five among running backs in carries, " +
			"rushing yards, touches, yards and fantasy points in each of the past two seasons.",
		SeasonStats: []playerprofile.Stat{
			{Label: "Rushing Attempts", Value: 102, Rank: ptr(3)},
			{Label: "Rush Yards", Value: 580, Rank: ptr(1)},
			{Label: "Rushing Touchdowns", Value: 4, Rank: ptr(8)},
			{Label: "YDS/RA", Value: 5.685999870300293, Rank: ptr(3)},
		},
	}
}

func ptr[T any](value T) *T { return &value }
