package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestRestOfSeasonFromStats(t *testing.T) {
	points := func(value float64) *float64 { return &value }
	stats := []statResponse{
		{SourceID: 0, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 6},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 6, AppliedTotal: points(20), Stats: map[string]float64{"rushingYards": 1}},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 7, AppliedTotal: points(20), Stats: map[string]float64{}},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 8, AppliedTotal: points(1.26), Stats: map[string]float64{"rushingYards": 1}},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 9, AppliedTotal: points(2.25), Stats: map[string]float64{"rushingYards": 1}},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 10, Stats: map[string]float64{"rushingYards": 1}},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2025, ScoringPeriodID: 11, AppliedTotal: points(100), Stats: map[string]float64{"rushingYards": 1}},
		{SourceID: 1, SplitTypeID: 0, SeasonID: 2026, ScoringPeriodID: 12, AppliedTotal: points(100), Stats: map[string]float64{"rushingYards": 1}},
	}
	restPoints, weeks := restOfSeasonFromStats(stats, 2026, 6)
	if restPoints == nil || *restPoints != 3.5 || weeks == nil || *weeks != 2 {
		t.Fatalf("rest-of-season result = %v, %v", restPoints, weeks)
	}
}

func TestRestOfSeasonFromStatsStopsAtWeek17(t *testing.T) {
	points := func(value float64) *float64 { return &value }
	stats := []statResponse{
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 17, AppliedTotal: points(10), Stats: map[string]float64{"rushingYards": 1}},
		{SourceID: 1, SplitTypeID: 1, SeasonID: 2026, ScoringPeriodID: 18, AppliedTotal: points(100), Stats: map[string]float64{"rushingYards": 1}},
	}
	restPoints, weeks := restOfSeasonFromStats(stats, 2026, 17)
	if restPoints == nil || *restPoints != 10 || weeks == nil || *weeks != 1 {
		t.Fatalf("week 18 included in result = %v, %v", restPoints, weeks)
	}
}

func TestRestOfSeasonFromStatsWithoutCountedWeeksIsUnknown(t *testing.T) {
	restPoints, weeks := restOfSeasonFromStats(nil, 2026, 6)
	if restPoints != nil || weeks != nil {
		t.Fatalf("empty projection result = %v, %v", restPoints, weeks)
	}
}

func TestProjectRestOfSeasonUsesWeeklyFilterAndMapsPlayers(t *testing.T) {
	fixture, err := os.ReadFile("testdata/rest_of_season_players.json")
	if err != nil {
		t.Fatal(err)
	}
	var receivedFilter struct {
		Players struct {
			IDs struct {
				Value []int `json:"value"`
			} `json:"filterIds"`
			SplitTypes struct {
				Value []int `json:"value"`
			} `json:"filterStatsForSplitTypeIds"`
			Periods struct {
				Value []int `json:"value"`
			} `json:"filterStatsForCurrentSeasonScoringPeriodId"`
		} `json:"players"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seasons/2026/segments/0/leagues/123456" ||
			r.URL.Query().Get("view") != "kona_player_info" ||
			r.Header.Get("Cookie") != "espn_s2=s2-test; SWID={swid-test}" {
			t.Errorf("request = %s, query=%v, cookie=%q", r.URL.Path, r.URL.Query(), r.Header.Get("Cookie"))
		}
		if err := json.Unmarshal([]byte(r.Header.Get("x-fantasy-filter")), &receivedFilter); err != nil {
			t.Errorf("invalid filter: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	projections, err := client.ProjectRestOfSeason(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123456", Season: 2026},
	}, fantasy.RestOfSeasonQuery{
		Week: 6, ExternalPlayerIDs: []string{"4241389", "4430878", "3916387", "4362628", "-5", "unknown", "4241389"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedIDs := []int{4241389, 4430878, 3916387, 4362628, -5}
	expectedPeriods := []int{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
	if !slices.Equal(receivedFilter.Players.IDs.Value, expectedIDs) ||
		!slices.Equal(receivedFilter.Players.SplitTypes.Value, []int{1}) ||
		!slices.Equal(receivedFilter.Players.Periods.Value, expectedPeriods) {
		t.Fatalf("projector filter = %+v", receivedFilter)
	}
	expected := map[string]fantasy.RestOfSeasonProjection{
		"4241389": {Points: 205.1, Weeks: 11},
		"4430878": {Points: 202.5, Weeks: 10},
		"3916387": {Points: 187.0, Weeks: 10},
		"4362628": {Points: 242.3, Weeks: 11},
	}
	if !equalRestOfSeasonProjections(projections, expected) {
		t.Fatalf("projections = %+v, want %+v", projections, expected)
	}
}

func TestProjectRestOfSeasonDoesNotRequestWhenIDsAreNotNumeric(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	projections, err := client.ProjectRestOfSeason(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123456", Season: 2026},
	}, fantasy.RestOfSeasonQuery{Week: 6, ExternalPlayerIDs: []string{"not-a-number"}})
	if err != nil || len(projections) != 0 || requests != 0 {
		t.Fatalf("empty player request result=%+v err=%v requests=%d", projections, err, requests)
	}
}

func TestProjectRestOfSeasonDoesNotExposeHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private response details", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	_, err := client.ProjectRestOfSeason(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123456", Season: 2026},
	}, fantasy.RestOfSeasonQuery{Week: 6, ExternalPlayerIDs: []string{"4241389"}})
	if err == nil || strings.Contains(err.Error(), "private response details") || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("safe HTTP error = %v", err)
	}
}

func equalRestOfSeasonProjections(
	got, want map[string]fantasy.RestOfSeasonProjection,
) bool {
	if len(got) != len(want) {
		return false
	}
	for playerID, expected := range want {
		if got[playerID] != expected {
			return false
		}
	}
	return true
}
