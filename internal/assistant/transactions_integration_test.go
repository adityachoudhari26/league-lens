//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

type assistantTransactionsSource struct {
	assistantLeagueSource
	transactions []fantasy.Transaction
	weeks        []int
	err          error
}

func (s *assistantTransactionsSource) ListTransactions(
	_ context.Context,
	_ fantasy.League,
	weeks []int,
) ([]fantasy.Transaction, error) {
	s.weeks = append([]int(nil), weeks...)
	return s.transactions, s.err
}

func TestGetTransactionsUsesOwnerNamesCatalogAndRecentWeeks(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "transactions-fixture", Season: 2026}
	data := transactionsFixtureData(now, ref)
	importService := importer.Service{DB: db, Source: assistantLeagueSource{data: data}}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: "transactions@example.test", ExternalAccountID: "sleeper",
		Selection: fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	source := &assistantTransactionsSource{
		assistantLeagueSource: assistantLeagueSource{data: data},
		transactions:          transactionsFixtureActivity(),
	}
	reader := dashboard.New(db, "transactions@example.test", nil, nil, nil)
	service := &Service{
		DB: db, OwnerEmail: "transactions@example.test", Dashboard: reader,
		ProviderSources: assistantFreeAgentSourceFactory{source: source},
		PlayerCatalog: playerids.Catalog{
			"stored-player":  {Name: "Catalog Name Must Lose", Position: "QB"},
			"catalog-player": {Name: "Catalog Player", Position: "RB"},
			"old-player":     {Name: "Old Catalog Player", Position: "TE"},
		},
	}
	tools := service.chatTools()
	leagues := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if leagues.IsError {
		t.Fatalf("list_leagues: %s", leagues.Content)
	}
	leagueID := findJSONField(t, leagues.Content, "id")

	result := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "activity", Name: "get_transactions",
		Input: json.RawMessage(fmt.Sprintf(
			`{"league_season_id":%q,"weeks_back":2,"limit":3}`, leagueID,
		)),
	})
	if result.IsError {
		t.Fatalf("get_transactions: %s", result.Content)
	}
	want := `{"transactions":[` +
		`{"date":"2026-10-18T12:00:00Z","type":"trade","teams":["Away Team","Home Team"],"players":[` +
		`{"name":"Database Player","position":"QB","action":"received","team":"Home Team","from_team":"Away Team"},` +
		`{"name":"Catalog Player","position":"RB","action":"received","team":"Away Team","from_team":"Home Team"}]},` +
		`{"date":"2026-10-17T14:00:00Z","type":"waiver","team":"Away Team","players":[` +
		`{"name":"Stored Defense","position":"DST","action":"added"},` +
		`{"name":"NYJ D/ST","position":"DST","action":"added"}],"faab_bid":5},` +
		`{"date":"2026-10-16T10:00:00Z","type":"add","team":"Home Team","players":[` +
		`{"name":"Database Player","position":"QB","action":"added"}]}]}`
	if result.Content != want {
		t.Fatalf("get_transactions = %s, want %s", result.Content, want)
	}
	if len(source.weeks) != 2 || source.weeks[0] != 5 || source.weeks[1] != 6 {
		t.Fatalf("requested scoring weeks = %v, want [5 6]", source.weeks)
	}

	unknownLeague := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "unknown-league", Name: "get_transactions",
		Input: json.RawMessage(`{"league_season_id":"00000000-0000-0000-0000-000000000001"}`),
	})
	if !unknownLeague.IsError || unknownLeague.Content != "requested data not found" {
		t.Fatalf("unknown league result = %+v", unknownLeague)
	}

	source.err = errors.New("private provider response")
	providerFailure := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "provider-failure", Name: "get_transactions",
		Input: json.RawMessage(fmt.Sprintf(`{"league_season_id":%q}`, leagueID)),
	})
	if !providerFailure.IsError || providerFailure.Content != "couldn't load league activity from Sleeper" {
		t.Fatalf("provider failure result = %+v", providerFailure)
	}
}

func transactionsFixtureData(now time.Time, ref fantasy.LeagueRef) fantasy.LeagueData {
	return fantasy.LeagueData{
		League: fantasy.League{Ref: ref, Name: "Transactions Fixture", TeamCount: 2},
		Teams: []fantasy.Team{
			{ExternalID: "home", Name: "Home Team"},
			{ExternalID: "away", Name: "Away Team"},
		},
		Players: []fantasy.Player{
			{ExternalID: "stored-player", Name: "Database Player", Kind: fantasy.PlayerPerson},
			{ExternalID: "stored-defense", Name: "Stored Defense", Kind: fantasy.PlayerTeamDefense},
		},
		Rosters: []fantasy.RosterSnapshot{
			{
				TeamID: "home", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{
					{PlayerID: "stored-player", Slot: fantasy.SlotQB, EligiblePositions: []fantasy.Position{fantasy.PositionQB}},
					{PlayerID: "stored-defense", Slot: fantasy.SlotDefense, EligiblePositions: []fantasy.Position{fantasy.PositionDEF}},
				},
			},
			{
				TeamID: "away", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
			},
		},
		FetchedAt: now,
	}
}

func transactionsFixtureActivity() []fantasy.Transaction {
	return []fantasy.Transaction{
		{
			ExternalID: "unknown",
			Type:       fantasy.TransactionDrop,
			ExecutedAt: time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC),
			Moves:      []fantasy.TransactionMove{{ExternalPlayerID: "unmapped", FromTeamID: "home"}},
		},
		{
			ExternalID: "trade",
			Type:       fantasy.TransactionTrade,
			ExecutedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
			Moves: []fantasy.TransactionMove{
				{ExternalPlayerID: "stored-player", FromTeamID: "away", ToTeamID: "home"},
				{ExternalPlayerID: "catalog-player", FromTeamID: "home", ToTeamID: "away"},
				{ExternalPlayerID: "unmapped", FromTeamID: "home", ToTeamID: "away"},
			},
		},
		{
			ExternalID: "waiver",
			Type:       fantasy.TransactionWaiver,
			ExecutedAt: time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC),
			FAABBid:    intPointer(5),
			Moves: []fantasy.TransactionMove{
				{ExternalPlayerID: "stored-defense", ToTeamID: "away", DefenseTeam: "GB"},
				{ExternalPlayerID: "NYJ", ToTeamID: "away", DefenseTeam: "NYJ"},
			},
		},
		{
			ExternalID: "add",
			Type:       fantasy.TransactionAdd,
			ExecutedAt: time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC),
			Moves:      []fantasy.TransactionMove{{ExternalPlayerID: "stored-player", ToTeamID: "home"}},
		},
		{
			ExternalID: "limited",
			Type:       fantasy.TransactionAdd,
			ExecutedAt: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC),
			Moves:      []fantasy.TransactionMove{{ExternalPlayerID: "old-player", ToTeamID: "home"}},
		},
	}
}

func intPointer(value int) *int { return &value }
