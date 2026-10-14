package dashboard

import (
	"testing"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCategorizePlayerNews(t *testing.T) {
	tests := []struct {
		headline string
		want     v1.NewsCategory
	}{
		{headline: "Backup returns to practice", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "The backup is expected to start", want: v1.NewsCategory_NEWS_CATEGORY_GENERAL},
		{headline: "Placed on injured reserve", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Player signed to the practice squad", want: v1.NewsCategory_NEWS_CATEGORY_TRANSACTION},
		{headline: "Player signed by the team", want: v1.NewsCategory_NEWS_CATEGORY_TRANSACTION},
		{headline: "Bounces back with two scores", want: v1.NewsCategory_NEWS_CATEGORY_GENERAL},
		{headline: "Moore (back) didn't practice", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Wilson (finger) ruled out", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Lamb dealing with quad bruise", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Smith (calf) limited Wednesday", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Coleman caught a seven-yard touchdown pass (on two targets) during the win", want: v1.NewsCategory_NEWS_CATEGORY_GENERAL},
		{headline: "St. Brown (not injury related) won't practice", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Smith-Njigba (hamstring) limited", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "IR role change discussed", want: v1.NewsCategory_NEWS_CATEGORY_INJURY},
		{headline: "Their best week is ahead", want: v1.NewsCategory_NEWS_CATEGORY_GENERAL},
	}
	for _, test := range tests {
		if got := categorize(test.headline); got != test.want {
			t.Errorf("categorize(%q) = %s, want %s", test.headline, got, test.want)
		}
	}
}

func TestPlayerUpdateTierOrderingAndDedupe(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	players := []store.PlayerNews{
		playerNewsRow(1, "general-bench-old", "Bench player update", now.Add(-time.Hour)),
		playerNewsRow(2, "starter-general", "Starter has a new role", now.Add(-2*time.Hour)),
		playerNewsRow(3, "starter-injury", "Starter is questionable", now.Add(-3*time.Hour)),
		playerNewsRow(3, "starter-injury-latest", "Starter outlook improves", now),
		playerNewsRow(4, "designated-bench", "Roster update", now.Add(-time.Minute)),
		playerNewsRow(5, "general-bench-new", "Another bench player update", now),
		playerNewsRow(6, "designated-starter", "Starter is out", now.Add(-time.Hour)),
	}
	summaries := map[string]*playerSummary{
		playerNewsRow(1, "", "", now).PlayerID.String(): {name: "Bench A", injuryStatus: v1.InjuryStatus_INJURY_STATUS_ACTIVE},
		playerNewsRow(2, "", "", now).PlayerID.String(): {name: "Starter A", starter: true, injuryStatus: v1.InjuryStatus_INJURY_STATUS_ACTIVE},
		playerNewsRow(3, "", "", now).PlayerID.String(): {name: "Starter B", starter: true, injuryStatus: v1.InjuryStatus_INJURY_STATUS_UNKNOWN},
		playerNewsRow(4, "", "", now).PlayerID.String(): {name: "Bench B", injuryStatus: v1.InjuryStatus_INJURY_STATUS_DOUBTFUL},
		playerNewsRow(5, "", "", now).PlayerID.String(): {name: "Bench C", injuryStatus: v1.InjuryStatus_INJURY_STATUS_UNKNOWN},
		playerNewsRow(6, "", "", now).PlayerID.String(): {name: "Designated starter", starter: true, injuryStatus: v1.InjuryStatus_INJURY_STATUS_OUT},
	}

	updates := rankPlayerUpdates(players, summaries)
	if len(updates) != 6 {
		t.Fatalf("updates = %d, want one update per player", len(updates))
	}
	wantPlayers := []string{"Designated starter", "Bench B", "Starter B", "Starter A", "Bench C", "Bench A"}
	for i, update := range updates {
		if update.PlayerName != wantPlayers[i] {
			t.Errorf("update %d player = %q, want %q", i, update.PlayerName, wantPlayers[i])
		}
	}
	if updates[2].Id != "espn:starter-injury-latest:"+updates[2].PlayerId {
		t.Fatalf("deduplicated update = %s, want the latest item", updates[2].Id)
	}
}

func TestPlayerSummaryKeepsWorstInjuryStatusAcrossTeams(t *testing.T) {
	playerID := testUUID(8)
	firstTeam := store.OverviewTeamsRow{ID: testUUID(1), Name: "First", Provider: "espn", Coverage: "full"}
	secondTeam := store.OverviewTeamsRow{ID: testUUID(2), Name: "Second", Provider: "sleeper", Coverage: "partial"}
	summaries := map[string]*playerSummary{}
	addPlayerSummary(summaries, firstTeam, store.ListSnapshotRosterForUserRow{
		PlayerID: playerID, DisplayName: "Player", SlotLabel: pgtype.Text{String: "BENCH", Valid: true},
		InjuryStatus: pgtype.Text{String: "out", Valid: true},
	})
	addPlayerSummary(summaries, secondTeam, store.ListSnapshotRosterForUserRow{
		PlayerID: playerID, DisplayName: "Player", SlotLabel: pgtype.Text{String: "QB", Valid: true},
		InjuryStatus: pgtype.Text{String: "ir", Valid: true},
	})
	summary := summaries[playerID.String()]
	if summary.injuryStatus != v1.InjuryStatus_INJURY_STATUS_INJURED_RESERVE || summary.starter || len(summary.teams) != 2 {
		t.Fatalf("summary = %+v, want worst status IR, no partial-coverage starter, two teams", summary)
	}
}

func playerNewsRow(id byte, itemID, headline string, published time.Time) store.PlayerNews {
	return store.PlayerNews{
		Source:       "espn",
		SourceItemID: itemID,
		PlayerID:     testUUID(id),
		Headline:     headline,
		Summary:      "Summary",
		Url:          "https://example.test/player",
		SourceLabel:  "RotoWire via ESPN",
		PublishedAt:  pgtype.Timestamptz{Time: published, Valid: true},
	}
}

func testUUID(value byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{value}, Valid: true}
}
