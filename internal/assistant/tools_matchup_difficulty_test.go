package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestMatchupDifficultyToolValidationAndRegistration(t *testing.T) {
	provider := &matchupDifficultyTestProvider{}
	service := &Service{ProviderSources: matchupDifficultyProviderSources{provider: provider}}
	tools := service.chatTools()
	if findAssistantToolByName((&Service{}).chatTools(), "get_matchup_difficulty") != nil {
		t.Fatal("matchup difficulty tool was registered without provider sources")
	}
	tool := findAssistantToolByName(tools, "get_matchup_difficulty")
	if tool == nil || tool.label(json.RawMessage(`{}`)) != "Checking matchups" {
		t.Fatal("matchup difficulty tool is missing or mislabeled")
	}
	if tool.definition.Description != getMatchupDifficultyDescription {
		t.Fatalf("description = %q", tool.definition.Description)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"unknown property", `{"league_season_id":"00000000-0000-0000-0000-000000000001","extra":true}`, "invalid tool input"},
		{
			"too many teams",
			`{"league_season_id":"00000000-0000-0000-0000-000000000001","teams":["KC","BUF","SF","DET","MIA","NYJ","NE","DAL","PHI"]}`,
			"pass at most 8 teams",
		},
		{"bad week", `{"league_season_id":"00000000-0000-0000-0000-000000000001","week":19}`, "week must be between 1 and 18"},
		{"bad position", `{"league_season_id":"00000000-0000-0000-0000-000000000001","position":"FLEX"}`, "position must be QB, RB, WR, TE, K, or DST"},
		{"bad league", `{"league_season_id":"not-a-uuid"}`, "invalid tool input"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runAssistantTool(context.Background(), tools, llm.ToolCall{
				ID: "invalid", Name: "get_matchup_difficulty", Input: json.RawMessage(test.input),
			})
			if !result.IsError || result.Content != test.want {
				t.Fatalf("result = %+v, want %q", result, test.want)
			}
		})
	}

	request, err := parseMatchupDifficultyInput(matchupDifficultyToolInput{
		LeagueSeasonID: "00000000-0000-0000-0000-000000000001",
		Teams:          []string{"kc", "WSH", "jac", "WAS", "JAX"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.teams, []string{"KC", "WAS", "JAX"}) {
		t.Fatalf("normalized teams = %v", request.teams)
	}
}

func TestMatchupPositionResolvesStoredAndCatalogPositions(t *testing.T) {
	catalog := playerids.Catalog{
		"456": {Position: "DEF"},
		"789": {Position: "PK"},
		"987": {Position: "PN"},
	}
	tests := []struct {
		name      string
		stored    string
		sleeperID string
		want      string
	}{
		{name: "stored defense", stored: "DEF", sleeperID: "123", want: "DST"},
		{name: "stored D slash ST", stored: "D/ST", sleeperID: "123", want: "DST"},
		{name: "stored DST", stored: "DST", sleeperID: "123", want: "DST"},
		{name: "stored PK", stored: "PK", sleeperID: "123", want: "K"},
		{name: "stored receiver", stored: "WR", sleeperID: "123", want: "WR"},
		{name: "Sleeper defense ID", sleeperID: "SF", want: "DST"},
		{name: "catalog defense", sleeperID: "456", want: "DST"},
		{name: "catalog kicker", sleeperID: "789", want: "K"},
		{name: "catalog punter skipped", sleeperID: "987"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchupPosition(test.stored, test.sleeperID, catalog); got != test.want {
				t.Fatalf("matchupPosition(%q, %q) = %q, want %q", test.stored, test.sleeperID, got, test.want)
			}
		})
	}
}

func TestBuildMatchupDifficultyResultHandlesRatingsByPlayer(t *testing.T) {
	table := matchupDifficultyTestTable()
	rosterWeek := matchupDifficultyRosterWeek{
		week: 6,
		players: []store.MatchupDifficultyOwnerRosterRow{
			{PlayerName: "Puka Nacua", Position: "WR", ProTeam: "LAR", SleeperID: "123"},
			{PlayerName: "Bye Back", Position: "RB", ProTeam: "KC", SleeperID: "124"},
			{PlayerName: "Unrated Tight End", Position: "TE", ProTeam: "WAS", SleeperID: "125"},
			{PlayerName: "Defense", ProTeam: "SF", SleeperID: "SF"},
			{PlayerName: "Unknown Player", SleeperID: "999"},
			{PlayerName: "Teamless Quarterback", Position: "QB", SleeperID: "126"},
		},
		games: []store.AssistantScheduleGamesRow{
			{Team: "LAR", Opponent: "BUF"},
			{Team: "WAS", Opponent: "NYG"},
			{Team: "SF", Opponent: "KC"},
		},
	}
	result := buildMatchupDifficultyResult(rosterWeek, table, playerids.Catalog{}, "")
	if result.Week != 6 || result.Scoring != "ppr" || len(result.Players) != 5 {
		t.Fatalf("roster result = %+v", result)
	}
	if result.Players[0].Name != "Puka Nacua" ||
		result.Players[0].Opponent != "BUF" ||
		*result.Players[0].PointsAllowedPerGame != 37.7 ||
		*result.Players[0].LeagueAverage != 31.2 ||
		*result.Players[0].Rank != 27 {
		t.Fatalf("rated receiver = %+v", result.Players[0])
	}
	if result.Players[1].Opponent != "BYE" ||
		result.Players[1].PointsAllowedPerGame != nil ||
		result.Players[1].LeagueAverage != nil || result.Players[1].Rank != nil {
		t.Fatalf("bye player = %+v", result.Players[1])
	}
	if result.Players[2].Opponent != "NYG" ||
		result.Players[2].PointsAllowedPerGame != nil ||
		result.Players[2].LeagueAverage != nil || result.Players[2].Rank != nil {
		t.Fatalf("missing opponent rating was not omitted: %+v", result.Players[2])
	}
	if result.Players[3].Position != "DST" || result.Players[3].Opponent != "KC" {
		t.Fatalf("Sleeper defense player = %+v", result.Players[3])
	}
	if result.Players[4].Opponent != "BYE" ||
		result.Players[4].PointsAllowedPerGame != nil ||
		result.Players[4].LeagueAverage != nil || result.Players[4].Rank != nil {
		t.Fatalf("teamless player = %+v", result.Players[4])
	}

	filtered := buildMatchupDifficultyResult(rosterWeek, table, playerids.Catalog{}, "WR")
	if len(filtered.Players) != 1 || filtered.Players[0].Position != "WR" {
		t.Fatalf("position-filtered players = %+v", filtered.Players)
	}
	empty := buildMatchupDifficultyResult(matchupDifficultyRosterWeek{week: 6}, table, nil, "")
	encoded, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"week":6,"scoring":"ppr"}` {
		t.Fatalf("empty roster output = %s", encoded)
	}
	emptyTeams := buildMatchupDifficultyDefenses(table, nil, "")
	if emptyTeams != nil {
		t.Fatalf("empty defenses = %#v", emptyTeams)
	}
}

func TestMatchupDifficultyTeamsProduceOneOrSixRows(t *testing.T) {
	table := matchupDifficultyTestTable()
	specific := buildMatchupDifficultyDefenses(table, []string{"NE"}, "WR")
	if len(specific) != 1 || specific[0].Team != "NE" || specific[0].Position != "WR" ||
		*specific[0].PointsAllowedPerGame != 31.4 {
		t.Fatalf("specific defense = %+v", specific)
	}
	all := buildMatchupDifficultyDefenses(table, []string{"NE"}, "")
	if len(all) != 6 || all[0].Position != "QB" || all[5].Position != "DST" {
		t.Fatalf("all positions = %+v", all)
	}
	if unknown := unknownMatchupDifficultyTeam([]string{"NE", "XYZ"}, table); unknown != "XYZ" {
		t.Fatalf("unknown team = %q", unknown)
	}
}

func TestMatchupRatingsCacheSuccessAndFailure(t *testing.T) {
	target := leaguesync.Target{Selection: fantasy.ConnectionSelection{
		League: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league", Season: 2026},
	}}
	provider := &matchupDifficultyTestProvider{table: matchupDifficultyTestTable()}
	service := &Service{ProviderSources: matchupDifficultyProviderSources{provider: provider}}
	matchupDifficultyAssertSuccessfulCache(t, service, provider, target)

	failedProvider := &matchupDifficultyTestProvider{ratingErr: errors.New("provider response included private details")}
	failedService := &Service{ProviderSources: matchupDifficultyProviderSources{provider: failedProvider}}
	matchupDifficultyAssertFailedFetchNotCached(t, failedService, failedProvider, target)
}

func matchupDifficultyAssertSuccessfulCache(
	t *testing.T,
	service *Service,
	provider *matchupDifficultyTestProvider,
	target leaguesync.Target,
) {
	t.Helper()
	for range 2 {
		if _, err := service.cachedMatchupRatings(context.Background(), "league-id", target); err != nil {
			t.Fatal(err)
		}
	}
	if provider.ratingCalls != 1 || provider.describeCalls != 1 {
		t.Fatalf("successful provider calls = ratings:%d describe:%d", provider.ratingCalls, provider.describeCalls)
	}
}

func matchupDifficultyAssertFailedFetchNotCached(
	t *testing.T,
	service *Service,
	provider *matchupDifficultyTestProvider,
	target leaguesync.Target,
) {
	t.Helper()
	for range 2 {
		_, err := service.cachedMatchupRatings(context.Background(), "league-id", target)
		if !errors.Is(err, errMatchupRatingsUnavailable) || toolErrorMessage(err) != "matchup ratings couldn't be loaded" {
			t.Fatalf("provider failure = %v, safe message %q", err, toolErrorMessage(err))
		}
	}
	if provider.ratingCalls != 2 {
		t.Fatalf("failed provider result was cached after %d calls", provider.ratingCalls)
	}
}

func matchupDifficultyTestTable() fantasy.DefenseVsPositionTable {
	positions := make(map[string]fantasy.PositionDefense, len(matchupDifficultyPositions))
	for _, position := range matchupDifficultyPositions {
		positions[position] = fantasy.PositionDefense{
			LeagueAverage: 31.24,
			ByTeam: map[string]fantasy.DefenseRating{
				"BUF": {PointsAllowedPerGame: 37.74, Rank: 27},
				"NE":  {PointsAllowedPerGame: 31.36, Rank: 20},
			},
		}
	}
	return fantasy.DefenseVsPositionTable{Scoring: "ppr", Positions: positions}
}

type matchupDifficultyTestProvider struct {
	table         fantasy.DefenseVsPositionTable
	ratingErr     error
	ratingCalls   int
	describeCalls int
}

func (provider *matchupDifficultyTestProvider) DescribeLeague(
	_ context.Context,
	leagueID string,
) (fantasy.League, error) {
	provider.describeCalls++
	return fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: leagueID, Season: 2026},
	}, nil
}

func (provider *matchupDifficultyTestProvider) FetchLeague(
	context.Context,
	fantasy.League,
	fantasy.FetchRequest,
) (fantasy.LeagueData, error) {
	return fantasy.LeagueData{}, nil
}

func (provider *matchupDifficultyTestProvider) DefenseVsPosition(
	context.Context,
	fantasy.League,
) (fantasy.DefenseVsPositionTable, error) {
	provider.ratingCalls++
	return provider.table, provider.ratingErr
}

type matchupDifficultyProviderSources struct {
	provider fantasy.Provider
	err      error
}

func (sources matchupDifficultyProviderSources) SourceFor(leaguesync.Target) (fantasy.Provider, error) {
	return sources.provider, sources.err
}
