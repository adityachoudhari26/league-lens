package sleeper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestListTransactionsMapsCompletedTransactions(t *testing.T) {
	fixture, err := os.ReadFile("testdata/transactions_week5.json")
	if err != nil {
		t.Fatal(err)
	}
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		switch r.URL.Path {
		case "/league/league-1/transactions/5":
			_, _ = w.Write(fixture)
		case "/league/league-1/transactions/7":
			_, _ = w.Write([]byte("null"))
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.BaseURL = server.URL
	transactions, err := client.ListTransactions(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-1", Season: 2026},
	}, []int{5, 7})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requestedPaths, []string{
		"/league/league-1/transactions/5", "/league/league-1/transactions/7",
	}) {
		t.Fatalf("requested paths = %v", requestedPaths)
	}
	if len(transactions) != 4 {
		t.Fatalf("got %d transactions, want 4: %+v", len(transactions), transactions)
	}

	byID := make(map[string]fantasy.Transaction, len(transactions))
	for _, transaction := range transactions {
		byID[transaction.ExternalID] = transaction
	}
	waiver := byID["1415954912793186304"]
	if waiver.Type != fantasy.TransactionWaiver || len(waiver.Moves) != 2 ||
		waiver.Moves[0] != (fantasy.TransactionMove{ExternalPlayerID: "11539", FromTeamID: "10"}) ||
		waiver.Moves[1] != (fantasy.TransactionMove{ExternalPlayerID: "3451", ToTeamID: "10"}) ||
		!waiver.ExecutedAt.Equal(time.UnixMilli(1791961780510).UTC()) || waiver.FAABBid != nil {
		t.Fatalf("waiver mapping = %+v", waiver)
	}
	addOnlyWaiver := byID["1415756339266801664"]
	if addOnlyWaiver.Type != fantasy.TransactionWaiver || len(addOnlyWaiver.Moves) != 1 ||
		addOnlyWaiver.Moves[0].ToTeamID != "1" {
		t.Fatalf("add-only waiver = %+v", addOnlyWaiver)
	}
	dropOnly := byID["1415755881798266880"]
	if dropOnly.Type != fantasy.TransactionDrop || len(dropOnly.Moves) != 1 ||
		dropOnly.Moves[0].FromTeamID != "1" || dropOnly.Moves[0].ToTeamID != "" ||
		dropOnly.Moves[0].DefenseTeam != "JAX" {
		t.Fatalf("drop-only free agent = %+v", dropOnly)
	}
	freeAgent := byID["1415083758507278336"]
	if freeAgent.Type != fantasy.TransactionAdd || len(freeAgent.Moves) != 2 ||
		freeAgent.Moves[0] != (fantasy.TransactionMove{
			ExternalPlayerID: "13414", FromTeamID: "8",
		}) ||
		freeAgent.Moves[1] != (fantasy.TransactionMove{
			ExternalPlayerID: "ATL", ToTeamID: "8", DefenseTeam: "ATL",
		}) ||
		!freeAgent.ExecutedAt.Equal(time.UnixMilli(1791744768314).UTC()) {
		t.Fatalf("free-agent add/drop = %+v", freeAgent)
	}
	for _, transaction := range transactions {
		if transaction.ExternalID == "1415106934205349888" {
			t.Fatal("failed waiver was included")
		}
	}
}

func TestSleeperTransactionSupportsSyntheticTradeAndFAAB(t *testing.T) {
	// The live league has no trades or FAAB bids, so these edge cases use inline provider-shaped rows.
	trade, keep := completedSleeperTransaction(transactionResponse{
		Type:          "trade",
		Status:        "complete",
		TransactionID: "synthetic-trade",
		Adds:          map[string]int{"300": 2, "NYJ": 2},
		Drops:         map[string]int{"300": 1, "NYJ": 3, "dropped-only": 5},
	})
	if !keep || trade.Type != fantasy.TransactionTrade || len(trade.Moves) != 2 ||
		trade.Moves[0] != (fantasy.TransactionMove{ExternalPlayerID: "300", FromTeamID: "1", ToTeamID: "2"}) ||
		trade.Moves[1] != (fantasy.TransactionMove{ExternalPlayerID: "NYJ", FromTeamID: "3", ToTeamID: "2", DefenseTeam: "NYJ"}) {
		t.Fatalf("trade mapping = %+v, kept=%v", trade, keep)
	}

	bid := 12
	waiver, keep := completedSleeperTransaction(transactionResponse{
		Type:          "waiver",
		Status:        "complete",
		TransactionID: "synthetic-waiver",
		Adds:          map[string]int{"900": 6},
		Settings:      &transactionSettingsResponse{WaiverBid: &bid},
	})
	if !keep || waiver.Type != fantasy.TransactionWaiver || waiver.FAABBid == nil || *waiver.FAABBid != 12 {
		t.Fatalf("FAAB waiver = %+v, kept=%v", waiver, keep)
	}
}

func TestListTransactionsValidatesProviderAndWeeks(t *testing.T) {
	client := NewClient(nil)
	validLeague := fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-1", Season: 2026},
	}
	cases := []struct {
		name   string
		league fantasy.League
		weeks  []int
	}{
		{
			name: "wrong provider",
			league: fantasy.League{Ref: fantasy.LeagueRef{
				Provider: fantasy.ProviderESPN, ExternalID: "league-1", Season: 2026,
			}},
			weeks: []int{1},
		},
		{name: "empty id", league: fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, Season: 2026}}, weeks: []int{1}},
		{
			name: "invalid season",
			league: fantasy.League{Ref: fantasy.LeagueRef{
				Provider: fantasy.ProviderSleeper, ExternalID: "league-1", Season: 1800,
			}},
			weeks: []int{1},
		},
		{name: "week below range", league: validLeague, weeks: []int{0}},
		{name: "week above range", league: validLeague, weeks: []int{19}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := client.ListTransactions(context.Background(), testCase.league, testCase.weeks); err == nil {
				t.Fatal("invalid transaction request was accepted")
			}
		})
	}
}

func TestSleeperTransactionSettingsDecode(t *testing.T) {
	var entry transactionResponse
	if err := json.Unmarshal([]byte(`{"settings":{"waiver_bid":7}}`), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Settings == nil || entry.Settings.WaiverBid == nil || *entry.Settings.WaiverBid != 7 {
		t.Fatalf("decoded settings = %+v", entry.Settings)
	}
}

func TestListTransactionsDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private response body", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.BaseURL = server.URL
	_, err := client.ListTransactions(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-1", Season: 2026},
	}, []int{5})
	if err == nil || strings.Contains(err.Error(), "private response body") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}
