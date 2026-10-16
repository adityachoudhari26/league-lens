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
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestListTransactionsMapsExecutedTransactions(t *testing.T) {
	week3, err := os.ReadFile("testdata/transactions_week3.json")
	if err != nil {
		t.Fatal(err)
	}
	week4, err := os.ReadFile("testdata/transactions_week4.json")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string][]byte{"3": week3, "4": week4}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seasons/2026/segments/0/leagues/1002879736" ||
			!slices.Equal(r.URL.Query()["view"], []string{"mTransactions2", "mSettings"}) ||
			r.Header.Get("Cookie") != "espn_s2=s2-test; SWID={swid-test}" {
			t.Errorf("request path=%q query=%v cookie=%q", r.URL.Path, r.URL.Query(), r.Header.Get("Cookie"))
		}
		fixture, ok := fixtures[r.URL.Query().Get("scoringPeriodId")]
		if !ok {
			t.Errorf("unexpected scoring period %q", r.URL.Query().Get("scoringPeriodId"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	transactions, err := client.ListTransactions(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2026},
	}, []int{3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 5 {
		t.Fatalf("got %d transactions, want 5: %+v", len(transactions), transactions)
	}

	byID := make(map[string]fantasy.Transaction, len(transactions))
	for _, transaction := range transactions {
		byID[transaction.ExternalID] = transaction
	}
	trade := byID["cb5773f2-fe1c-4697-ad3a-59682dde3ca1"]
	if trade.Type != fantasy.TransactionTrade || len(trade.Moves) != 2 ||
		trade.Moves[0] != (fantasy.TransactionMove{ExternalPlayerID: "3121422", FromTeamID: "2", ToTeamID: "9"}) ||
		trade.Moves[1] != (fantasy.TransactionMove{ExternalPlayerID: "4569987", FromTeamID: "9", ToTeamID: "2"}) ||
		!trade.ExecutedAt.Equal(time.UnixMilli(1790718020901).UTC()) {
		t.Fatalf("trade mapping = %+v", trade)
	}
	if trade.FAABBid != nil {
		t.Fatalf("non-waiver transaction has FAAB bid: %+v", trade)
	}

	addDrop := byID["0a17a406-8362-4f57-89f1-2b79f3228fa6"]
	if addDrop.Type != fantasy.TransactionAdd || len(addDrop.Moves) != 2 ||
		addDrop.Moves[0].ExternalPlayerID != "4682745" || addDrop.Moves[0].FromTeamID != "" ||
		addDrop.Moves[0].ToTeamID != "4" || addDrop.Moves[1].ExternalPlayerID != "4239996" ||
		addDrop.Moves[1].FromTeamID != "4" || addDrop.Moves[1].ToTeamID != "" ||
		!addDrop.ExecutedAt.Equal(time.UnixMilli(1790898112679).UTC()) {
		t.Fatalf("free-agent add/drop mapping = %+v", addDrop)
	}
	addOnly := byID["2cd427a1-9382-420c-8781-8f6bff8632a3"]
	if addOnly.Type != fantasy.TransactionAdd || len(addOnly.Moves) != 1 {
		t.Fatalf("free-agent add-only mapping = %+v", addOnly)
	}
	waiver := byID["e2677f3f-92a4-4538-8356-2e9d130f9d20"]
	if waiver.Type != fantasy.TransactionWaiver || len(waiver.Moves) != 2 ||
		!waiver.ExecutedAt.Equal(time.UnixMilli(1790752267828).UTC()) || waiver.FAABBid != nil {
		t.Fatalf("waiver mapping = %+v", waiver)
	}
	defense := byID["c5aeb0c1-722d-44ec-aadb-033740b00384"]
	if defense.Moves[0].ExternalPlayerID != "-16033" || defense.Moves[0].DefenseTeam != "BAL" ||
		defense.Moves[1].ExternalPlayerID != "-16017" || defense.Moves[1].DefenseTeam != "NE" {
		t.Fatalf("defense moves = %+v", defense.Moves)
	}
	if _, ok := byID["bbc41609-3b29-4c02-9d8a-c40b070ced3a"]; ok {
		t.Fatal("pending trade proposal was included")
	}
	if _, ok := byID["82025153-4ac0-4c44-b66d-205cac557f20"]; ok {
		t.Fatal("canceled waiver was included")
	}
}

func TestESPNTransactionFAABRequiresAcquisitionBudget(t *testing.T) {
	fixture, err := os.ReadFile("testdata/transactions_week4.json")
	if err != nil {
		t.Fatal(err)
	}
	var response leagueResponse
	if err := json.Unmarshal(fixture, &response); err != nil {
		t.Fatal(err)
	}
	waiver := response.Transactions[3]
	response.Settings.AcquisitionSettings.IsUsingAcquisitionBudget = true
	transaction, keep := executedTransaction(
		waiver, response.Settings.AcquisitionSettings.IsUsingAcquisitionBudget,
	)
	if !keep || transaction.FAABBid == nil || *transaction.FAABBid != 0 {
		t.Fatalf("budgeted waiver = %+v, kept=%v", transaction, keep)
	}
}

func TestESPNDropOnlyFreeAgentUsesDropType(t *testing.T) {
	transaction, keep := executedTransaction(transactionResponse{
		ID:     "drop-only",
		Type:   "FREEAGENT",
		Status: "EXECUTED",
		Items:  []transactionItemResponse{{PlayerID: 123, Type: "DROP", FromTeamID: 4}},
	}, false)
	if !keep || transaction.Type != fantasy.TransactionDrop ||
		len(transaction.Moves) != 1 || transaction.Moves[0].ToTeamID != "" {
		t.Fatalf("drop-only free agent = %+v, kept=%v", transaction, keep)
	}
}

func TestListTransactionsDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private response body", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	_, err := client.ListTransactions(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123", Season: 2026},
	}, []int{4})
	if err == nil || strings.Contains(err.Error(), "private response body") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func TestListTransactionsValidatesProviderAndWeeks(t *testing.T) {
	client := NewClient(nil, Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	validLeague := fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123", Season: 2026},
	}
	cases := []struct {
		name   string
		league fantasy.League
		weeks  []int
	}{
		{name: "wrong provider", league: fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "123", Season: 2026}}, weeks: []int{1}},
		{name: "invalid id", league: fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "bad", Season: 2026}}, weeks: []int{1}},
		{name: "invalid season", league: fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123", Season: 1800}}, weeks: []int{1}},
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

	missingCredentials := NewClient(nil, Credentials{}, 2026)
	if _, err := missingCredentials.ListTransactions(context.Background(), validLeague, []int{1}); err == nil {
		t.Fatal("missing credentials were accepted")
	}
	wrongSeason := NewClient(nil, Credentials{S2: "s2-test", SWID: "swid-test"}, 2025)
	if _, err := wrongSeason.ListTransactions(context.Background(), validLeague, []int{1}); err == nil {
		t.Fatal("season mismatch was accepted")
	}
}
