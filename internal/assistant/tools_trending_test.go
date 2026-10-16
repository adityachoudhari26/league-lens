package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

type trendingSourceCall struct {
	kind          string
	lookbackHours int
	limit         int
}

type trendingSourceStub struct {
	rows  []sleeper.TrendingPlayer
	err   error
	calls []trendingSourceCall
}

func (s *trendingSourceStub) TrendingPlayers(
	_ context.Context,
	kind string,
	lookbackHours, limit int,
) ([]sleeper.TrendingPlayer, error) {
	s.calls = append(s.calls, trendingSourceCall{
		kind: kind, lookbackHours: lookbackHours, limit: limit,
	})
	return s.rows, s.err
}

func TestTrendingToolDefaultsMapsAndLimitsPlayers(t *testing.T) {
	source := &trendingSourceStub{rows: []sleeper.TrendingPlayer{
		{PlayerID: "unmapped", Count: 100},
		{PlayerID: "pk", Count: 90},
		{PlayerID: "qb", Count: 80},
		{PlayerID: "wr", Count: 70},
		{PlayerID: "rb", Count: 60},
	}}
	service := &Service{
		Trending: source,
		PlayerCatalog: playerids.Catalog{
			"pk": {Name: "Kicker", Position: "PK", Team: "FA"},
			"qb": {Name: "Quarterback", Position: "QB", Team: "BUF"},
			"wr": {Name: "Receiver", Position: "WR", Team: "KC"},
			"rb": {Name: "Running Back", Position: "RB", Team: "SF"},
		},
	}
	tools := service.chatTools()
	tool := findAssistantToolByName(tools, "get_trending_players")
	if tool == nil || tool.label(json.RawMessage(`{}`)) != "Checking trending players" {
		t.Fatal("trending tool is missing or has the wrong label")
	}
	const description = "Players other fantasy managers are adding or dropping most across Sleeper right now, " +
		"most-added (or most-dropped) first. Use it for waiver and pickup questions; " +
		"pass league_season_id to also see whether each player is available, on your team, or " +
		"rostered by another team in that league."
	if tool.definition.Description != description {
		t.Fatalf("tool description = %q", tool.definition.Description)
	}

	result := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "trending", Name: "get_trending_players",
		Input: json.RawMessage(`{"limit":3}`),
	})
	const expected = `{"type":"add","lookback_hours":24,"players":[` +
		`{"name":"Kicker","position":"K","nfl_team":"FA","count":90},` +
		`{"name":"Quarterback","position":"QB","nfl_team":"BUF","count":80},` +
		`{"name":"Receiver","position":"WR","nfl_team":"KC","count":70}]}`
	if result.IsError || result.Content != expected {
		t.Fatalf("trending result = %+v, want content %s", result, expected)
	}
	if len(source.calls) != 1 || source.calls[0] != (trendingSourceCall{
		kind: "add", lookbackHours: 24, limit: trendingFetchLimit,
	}) {
		t.Fatalf("source calls = %+v", source.calls)
	}
}

func TestTrendingToolReturnsEmptyPlayersAsArray(t *testing.T) {
	service := &Service{
		Trending:      &trendingSourceStub{rows: []sleeper.TrendingPlayer{{PlayerID: "unmapped"}}},
		PlayerCatalog: playerids.Catalog{},
	}
	result := runAssistantTool(context.Background(), service.chatTools(), llm.ToolCall{
		ID: "trending", Name: "get_trending_players", Input: json.RawMessage(`{}`),
	})
	const expected = `{"type":"add","lookback_hours":24,"players":[]}`
	if result.IsError || result.Content != expected {
		t.Fatalf("empty trending result = %+v, want content %s", result, expected)
	}
}

func TestMapTrendingPlayersNormalizesDefensePosition(t *testing.T) {
	players := mapTrendingPlayers(
		[]sleeper.TrendingPlayer{{PlayerID: "def", Count: 1}},
		playerids.Catalog{"def": {Name: "Defense", Position: "DEF"}},
		1,
	)
	if len(players) != 1 || players[0].Position != "DST" {
		t.Fatalf("defense position = %+v, want DST", players)
	}
}

func TestTrendingToolPassesDropAndLookback(t *testing.T) {
	source := &trendingSourceStub{rows: []sleeper.TrendingPlayer{
		{PlayerID: "a", Count: 2}, {PlayerID: "b", Count: 1},
	}}
	service := &Service{
		Trending: source,
		PlayerCatalog: playerids.Catalog{
			"a": {Name: "First", Position: "LB"},
			"b": {Name: "Second", Position: "CB"},
		},
	}
	result := runAssistantTool(context.Background(), service.chatTools(), llm.ToolCall{
		ID: "trending", Name: "get_trending_players",
		Input: json.RawMessage(`{"type":"drop","lookback_hours":72,"limit":2}`),
	})
	var output trendingToolResult
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatal(err)
	}
	if result.IsError || output.Type != "drop" || output.LookbackHours != 72 ||
		len(output.Players) != 2 || output.Players[0].Position != "LB" ||
		source.calls[0] != (trendingSourceCall{kind: "drop", lookbackHours: 72, limit: trendingFetchLimit}) {
		t.Fatalf("trending result=%+v source calls=%+v", result, source.calls)
	}
}

func TestTrendingToolRejectsInvalidInputs(t *testing.T) {
	service := &Service{
		Trending:      &trendingSourceStub{},
		PlayerCatalog: playerids.Catalog{},
	}
	tools := service.chatTools()
	tests := []struct {
		name  string
		input string
	}{
		{name: "unknown field", input: `{"extra":true}`},
		{name: "kind", input: `{"type":"foo"}`},
		{name: "zero hours", input: `{"lookback_hours":0}`},
		{name: "too many hours", input: `{"lookback_hours":169}`},
		{name: "zero limit", input: `{"limit":0}`},
		{name: "too many players", input: `{"limit":26}`},
		{name: "invalid league ID", input: `{"league_season_id":"nope"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runAssistantTool(context.Background(), tools, llm.ToolCall{
				ID: "invalid", Name: "get_trending_players", Input: json.RawMessage(test.input),
			})
			if !result.IsError || result.Content != "invalid tool input" {
				t.Fatalf("invalid input result = %+v", result)
			}
		})
	}
}

func TestTrendingToolCachesSuccessfulResponsesByKindAndLookback(t *testing.T) {
	source := &trendingSourceStub{rows: []sleeper.TrendingPlayer{{PlayerID: "qb", Count: 1}}}
	service := &Service{
		Trending: source,
		PlayerCatalog: playerids.Catalog{
			"qb": {Name: "Quarterback", Position: "QB"},
		},
	}
	call := func(input string) llm.ToolResult {
		t.Helper()
		return runAssistantTool(context.Background(), service.chatTools(), llm.ToolCall{
			ID: "trending", Name: "get_trending_players", Input: json.RawMessage(input),
		})
	}
	for _, input := range []string{`{}`, `{"type":"add"}`} {
		if result := call(input); result.IsError {
			t.Fatalf("cached call: %+v", result)
		}
	}
	if result := call(`{"lookback_hours":72}`); result.IsError {
		t.Fatalf("different lookback call: %+v", result)
	}
	if len(source.calls) != 2 {
		t.Fatalf("source fetched %d times, want 2", len(source.calls))
	}
	service.trending.entries[trendingCacheKey{kind: "add", lookbackHours: 24}] = trendingCacheEntry{
		players: source.rows, expires: time.Now().Add(-time.Second),
	}
	if result := call(`{}`); result.IsError {
		t.Fatalf("expired cache call: %+v", result)
	}
	if len(source.calls) != 3 {
		t.Fatalf("source fetched %d times after expiry, want 3", len(source.calls))
	}
	for _, call := range source.calls {
		if call.limit != trendingFetchLimit {
			t.Fatalf("fetch limit = %d, want %d", call.limit, trendingFetchLimit)
		}
	}
}

func TestTrendingToolDoesNotCacheErrorsOrExposeSourceDetails(t *testing.T) {
	source := &trendingSourceStub{err: errors.New("fake response body: private provider detail")}
	service := &Service{
		Trending:      source,
		PlayerCatalog: playerids.Catalog{},
	}
	for range 2 {
		result := runAssistantTool(context.Background(), service.chatTools(), llm.ToolCall{
			ID: "trending", Name: "get_trending_players", Input: json.RawMessage(`{}`),
		})
		if !result.IsError || result.Content != "couldn't load the requested data" ||
			strings.Contains(result.Content, "private provider detail") {
			t.Fatalf("source error result = %+v", result)
		}
	}
	if len(source.calls) != 2 {
		t.Fatalf("source fetched %d times after errors, want 2", len(source.calls))
	}
}

func TestTrendingToolRequiresPlayerCatalog(t *testing.T) {
	service := &Service{
		Trending: &trendingSourceStub{rows: []sleeper.TrendingPlayer{{PlayerID: "qb", Count: 1}}},
	}
	result := runAssistantTool(context.Background(), service.chatTools(), llm.ToolCall{
		ID: "trending", Name: "get_trending_players", Input: json.RawMessage(`{}`),
	})
	if !result.IsError || result.Content != "couldn't load the requested data" {
		t.Fatalf("missing catalog result = %+v", result)
	}
}

func TestTrendingToolIsAbsentWithoutSource(t *testing.T) {
	if findAssistantToolByName((&Service{}).chatTools(), "get_trending_players") != nil {
		t.Fatal("trending tool is registered without a source")
	}
}
