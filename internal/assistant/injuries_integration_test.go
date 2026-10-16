//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

func TestInjuryReportReadsOwnerRosterAndRequestedPlayers(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "injury-fixture", Season: 2026}
	importService := importer.Service{DB: db, Source: assistantLeagueSource{data: injuryFixtureData(now, ref)}}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: "injury@example.test", ExternalAccountID: "sleeper",
		Selection: fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	recent := now.Add(-24 * time.Hour).Truncate(time.Minute)
	source := &injurySourceStub{injuries: map[int64]*espn.AthleteInjury{
		101: {Status: "Questionable", Injury: "Hamstring", ReturnDate: "2026-10-25", Note: "Limited.", Updated: recent},
		102: nil,
		103: {Status: "Out", Injury: "Ankle", Updated: recent},
		104: {Status: "Doubtful", Updated: now.Add(-30 * 24 * time.Hour)},
		105: {Status: "Injured Reserve", Injury: "Knee", Updated: recent},
	}}
	reader := dashboard.New(db, "injury@example.test", nil, nil, nil)
	service := &Service{
		DB: db, OwnerEmail: "injury@example.test", Dashboard: reader, Injuries: source,
		PlayerCatalog: playerids.Catalog{"catalog-te": {Name: "Catalog TE", IDs: map[string]string{"sleeper": "catalog-te", "espn": "103"}}},
	}
	tools := service.chatTools()
	leagueID, homeTeamID, awayTeamID := injuryFixtureIDs(t, ctx, reader, tools)

	roster := runInjuryReport(t, tools, fmt.Sprintf(`{"league_season_id":%q}`, leagueID))
	wantRoster := []injuryReportPlayer{
		{Name: "Catalog TE", NFLTeam: "KC", Status: "Out", Injury: "Ankle", Updated: &recent},
		{Name: "Hurt WR", NFLTeam: "BUF", Status: "Questionable", Injury: "Hamstring", ReturnDate: "2026-10-25", Note: "Limited.", Updated: &recent},
	}
	assertInjuryPlayers(t, roster, wantRoster)
	if source.season != fantasy.CurrentSeason(time.Now()) || !sameAthletes(source.calls[0], []int64{103, 102, 101, 104}) {
		t.Fatalf("roster lookup season/athletes = %d/%v", source.season, source.calls[0])
	}

	healthyID := findPlayerIDByName(t, rosterContent(t, ctx, tools, homeTeamID), "Healthy RB")
	unmappedID := findPlayerIDByName(t, rosterContent(t, ctx, tools, homeTeamID), "Unmapped K")
	awayID := findPlayerIDByName(t, rosterContent(t, ctx, tools, awayTeamID), "Away WR")
	requested := runInjuryReport(t, tools, fmt.Sprintf(`{"player_ids":[%q,%q,%q]}`, healthyID, unmappedID, awayID))
	assertInjuryPlayers(t, requested, []injuryReportPlayer{
		{Name: "Away WR", NFLTeam: "WAS", Status: "IR", Injury: "Knee", Updated: &recent},
		{Name: "Healthy RB", NFLTeam: "MIA", Status: "Active"},
	})

	assertInjuryToolError(t, tools, `{"league_season_id":"00000000-0000-0000-0000-000000000001"}`, "requested data not found")
	assertInjuryToolError(t, tools, `{"player_ids":["00000000-0000-0000-0000-000000000001"]}`,
		"unknown player_id 00000000-0000-0000-0000-000000000001")
}

func injuryFixtureData(now time.Time, ref fantasy.LeagueRef) fantasy.LeagueData {
	espnID := func(id string) []fantasy.PlayerID {
		return []fantasy.PlayerID{{Namespace: fantasy.PlayerIDESPN, ExternalID: id}}
	}
	entry := func(playerID, proTeam string) fantasy.RosterEntry {
		return fantasy.RosterEntry{PlayerID: playerID, Slot: fantasy.SlotBench, ProTeam: proTeam}
	}
	return fantasy.LeagueData{
		League: fantasy.League{Ref: ref, Name: "Injury Fixture", TeamCount: 2}, FetchedAt: now,
		Teams: []fantasy.Team{{ExternalID: "home", Name: "Home Team"}, {ExternalID: "away", Name: "Away Team"}},
		Players: []fantasy.Player{
			{ExternalID: "hurt-wr", Name: "Hurt WR", Kind: fantasy.PlayerPerson, KnownIDs: espnID("101")},
			{ExternalID: "healthy-rb", Name: "Healthy RB", Kind: fantasy.PlayerPerson, KnownIDs: espnID("102")},
			{ExternalID: "catalog-te", Name: "Catalog TE", Kind: fantasy.PlayerPerson},
			{ExternalID: "stale-qb", Name: "Stale QB", Kind: fantasy.PlayerPerson, KnownIDs: espnID("104")},
			{ExternalID: "unmapped-k", Name: "Unmapped K", Kind: fantasy.PlayerPerson},
			{ExternalID: "away-wr", Name: "Away WR", Kind: fantasy.PlayerPerson, KnownIDs: espnID("105")},
		},
		Rosters: []fantasy.RosterSnapshot{
			{
				TeamID: "home", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{
					entry("hurt-wr", "BUF"), entry("healthy-rb", "MIA"), entry("catalog-te", "KC"),
					entry("stale-qb", "DAL"), entry("unmapped-k", "SEA"),
				},
			},
			{
				TeamID: "away", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{entry("away-wr", "WAS")},
			},
		},
	}
}

func injuryFixtureIDs(t *testing.T, ctx context.Context, reader *dashboard.Service, tools []assistantTool) (string, string, string) {
	t.Helper()
	leagues := runAssistantTool(ctx, tools, llm.ToolCall{ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`)})
	if leagues.IsError {
		t.Fatalf("list_leagues: %s", leagues.Content)
	}
	leagueID := findJSONField(t, leagues.Content, "id")
	page, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: leagueID})
	if err != nil {
		t.Fatal(err)
	}
	var homeTeamID, awayTeamID string
	for _, team := range page.GetTeams() {
		if team.GetSelected() {
			homeTeamID = team.GetId()
		} else {
			awayTeamID = team.GetId()
		}
	}
	return leagueID, homeTeamID, awayTeamID
}

func rosterContent(t *testing.T, ctx context.Context, tools []assistantTool, teamID string) string {
	t.Helper()
	roster := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "roster", Name: "get_team_roster", Input: json.RawMessage(`{"team_id":"` + teamID + `"}`),
	})
	if roster.IsError {
		t.Fatalf("get_team_roster: %s", roster.Content)
	}
	return roster.Content
}

func runInjuryReport(t *testing.T, tools []assistantTool, input string) []injuryReportPlayer {
	t.Helper()
	result := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "injuries", Name: "get_injury_report", Input: json.RawMessage(input),
	})
	if result.IsError {
		t.Fatalf("get_injury_report %s: %s", input, result.Content)
	}
	var report injuryReportResult
	if err := json.Unmarshal([]byte(result.Content), &report); err != nil {
		t.Fatal(err)
	}
	return report.Players
}

func assertInjuryPlayers(t *testing.T, got, want []injuryReportPlayer) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("players = %s\nwant %s", gotJSON, wantJSON)
	}
}

func assertInjuryToolError(t *testing.T, tools []assistantTool, input, want string) {
	t.Helper()
	result := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "injuries", Name: "get_injury_report", Input: json.RawMessage(input),
	})
	if !result.IsError || result.Content != want {
		t.Fatalf("input %s: result = %+v, want %q", input, result, want)
	}
}

func sameAthletes(got, want []int64) bool {
	got, want = slices.Clone(got), slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	return slices.Equal(got, want)
}
