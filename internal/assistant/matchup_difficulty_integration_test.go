//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestGetMatchupDifficultyReadsImportedRosterAndTeams(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{
		Provider: fantasy.ProviderSleeper, ExternalID: "matchup-difficulty-fixture",
		Season: fantasy.CurrentSeason(now),
	}
	data := matchupDifficultyIntegrationData(now, ref)
	source := &matchupDifficultyIntegrationProvider{
		data: data, table: matchupDifficultyIntegrationTable(),
	}
	importService := importer.Service{DB: db, Source: source}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: "matchup-difficulty@example.test", ExternalAccountID: "sleeper",
		Selection: fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	otherRef := ref
	otherRef.ExternalID = "other-matchup-difficulty-fixture"
	_, err = importService.Import(ctx, importer.Request{
		OwnerEmail: "matchup-difficulty-other@example.test", ExternalAccountID: "other-sleeper",
		Selection: fantasy.ConnectionSelection{League: otherRef, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	matchupDifficultySeedSchedule(ctx, t, db, ref.Season, now)

	reader := dashboard.New(db, "matchup-difficulty@example.test", nil, nil, nil)
	service := &Service{
		DB: db, OwnerEmail: "matchup-difficulty@example.test", Dashboard: reader,
		ProviderSources: matchupDifficultyIntegrationSources{provider: source},
		PlayerCatalog:   playerids.Catalog{},
	}
	tools := service.chatTools()
	leagues := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if leagues.IsError {
		t.Fatalf("list_leagues: %s", leagues.Content)
	}
	leagueID := findJSONField(t, leagues.Content, "id")
	roster := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "roster", Name: "get_matchup_difficulty",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `"}`),
	})
	if roster.IsError {
		t.Fatalf("get_matchup_difficulty roster: %s", roster.Content)
	}
	var result matchupDifficultyResult
	if err := json.Unmarshal([]byte(roster.Content), &result); err != nil {
		t.Fatal(err)
	}
	if result.Week != 6 || result.Scoring != "ppr" || len(result.Players) != 3 {
		t.Fatalf("roster result = %+v", result)
	}
	puka, found := matchupDifficultyPlayerNamed(result.Players, "Puka Nacua")
	if !found || puka.Position != "WR" || puka.NFLTeam != "LAR" || puka.Opponent != "BUF" ||
		puka.PointsAllowedPerGame == nil || *puka.PointsAllowedPerGame != 45.1 ||
		puka.LeagueAverage == nil || *puka.LeagueAverage != 31.2 || puka.Rank == nil || *puka.Rank != 32 {
		t.Fatalf("rated roster player = %+v", puka)
	}
	bye, found := matchupDifficultyPlayerNamed(result.Players, "Bye Back")
	if !found || bye.Opponent != "BYE" || bye.PointsAllowedPerGame != nil ||
		bye.LeagueAverage != nil || bye.Rank != nil {
		t.Fatalf("bye roster player = %+v", bye)
	}
	defense, found := matchupDifficultyPlayerNamed(result.Players, "San Francisco D/ST")
	if !found || defense.Position != "DST" || defense.Opponent != "KC" ||
		defense.PointsAllowedPerGame == nil || *defense.PointsAllowedPerGame != 10.3 ||
		defense.Rank == nil || *defense.Rank != 1 {
		t.Fatalf("Sleeper D/ST roster player = %+v", defense)
	}

	teams := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "teams", Name: "get_matchup_difficulty",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `","teams":["NE","WSH"],"position":"WR"}`),
	})
	if teams.IsError {
		t.Fatalf("get_matchup_difficulty teams: %s", teams.Content)
	}
	var teamResult matchupDifficultyResult
	if err := json.Unmarshal([]byte(teams.Content), &teamResult); err != nil {
		t.Fatal(err)
	}
	if teamResult.Scoring != "ppr" || len(teamResult.Defenses) != 2 ||
		teamResult.Defenses[0].Team != "NE" || teamResult.Defenses[0].Position != "WR" ||
		teamResult.Defenses[1].Team != "WAS" || teamResult.Defenses[1].Position != "WR" {
		t.Fatalf("specific teams result = %+v", teamResult)
	}
	if source.ratingCalls != 1 {
		t.Fatalf("cached provider fetch count = %d, want 1", source.ratingCalls)
	}

	otherService := &Service{
		DB: db, OwnerEmail: "matchup-difficulty-other@example.test",
		ProviderSources: matchupDifficultyIntegrationSources{provider: source},
	}
	request, err := parseMatchupDifficultyInput(matchupDifficultyToolInput{LeagueSeasonID: leagueID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherService.getMatchupDifficulty(ctx, request); err == nil ||
		connect.CodeOf(err) != connect.CodeNotFound || !strings.Contains(err.Error(), "league not found") {
		t.Fatalf("other owner's league error = %v", err)
	}
}

func matchupDifficultyIntegrationData(now time.Time, ref fantasy.LeagueRef) fantasy.LeagueData {
	entry := func(playerID string, position fantasy.Position, nflTeam string) fantasy.RosterEntry {
		return fantasy.RosterEntry{
			PlayerID: playerID, Slot: fantasy.SlotBench,
			EligiblePositions: []fantasy.Position{position}, ProTeam: nflTeam,
		}
	}
	return fantasy.LeagueData{
		League:    fantasy.League{Ref: ref, Name: "Matchup Difficulty Fixture", TeamCount: 2},
		FetchedAt: now,
		Teams:     []fantasy.Team{{ExternalID: "home", Name: "Home Team"}, {ExternalID: "away", Name: "Away Team"}},
		Players: []fantasy.Player{
			{ExternalID: "101", Name: "Puka Nacua", Kind: fantasy.PlayerPerson},
			{ExternalID: "102", Name: "Bye Back", Kind: fantasy.PlayerPerson},
			{ExternalID: "999", Name: "Unknown Position", Kind: fantasy.PlayerPerson},
			{ExternalID: "SF", Name: "San Francisco D/ST", Kind: fantasy.PlayerTeamDefense},
		},
		Rosters: []fantasy.RosterSnapshot{{
			TeamID: "home", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
			Coverage: fantasy.CoverageFull, ObservedAt: now,
			Entries: []fantasy.RosterEntry{
				entry("101", fantasy.PositionWR, "LAR"),
				entry("102", fantasy.PositionRB, "NE"),
				entry("999", "", "WAS"),
				{PlayerID: "SF", Slot: fantasy.SlotBench, ProTeam: "SF"},
			},
		}, {
			TeamID: "away", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
			Coverage: fantasy.CoverageFull, ObservedAt: now,
		}},
	}
}

func matchupDifficultyIntegrationTable() fantasy.DefenseVsPositionTable {
	return fantasy.DefenseVsPositionTable{
		Scoring: "ppr",
		Positions: map[string]fantasy.PositionDefense{
			"WR": {
				LeagueAverage: 31.179,
				ByTeam: map[string]fantasy.DefenseRating{
					"BUF": {PointsAllowedPerGame: 45.06, Rank: 32},
					"NE":  {PointsAllowedPerGame: 31.36, Rank: 20},
					"WAS": {PointsAllowedPerGame: 39.55, Rank: 29},
				},
			},
			"DST": {
				LeagueAverage: 8.14,
				ByTeam: map[string]fantasy.DefenseRating{
					"KC": {PointsAllowedPerGame: 10.25, Rank: 1},
				},
			},
		},
	}
}

func matchupDifficultySeedSchedule(ctx context.Context, t *testing.T, db *database.DB, season int, now time.Time) {
	t.Helper()
	games := [][2]string{
		{"LAR", "BUF"}, {"BUF", "LAR"},
		{"SF", "KC"}, {"KC", "SF"},
		{"WAS", "DAL"}, {"DAL", "WAS"},
	}
	err := db.InTx(ctx, func(q *store.Queries) error {
		for _, game := range games {
			if err := q.AddNFLGame(ctx, store.AddNFLGameParams{
				Season: int32(season), Week: 6, Team: game[0], Opponent: game[1],
				KickoffAt: pgtype.Timestamptz{Time: now.Add(7 * 24 * time.Hour), Valid: true},
				FetchedAt: pgtype.Timestamptz{Time: now, Valid: true},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func matchupDifficultyPlayerNamed(
	players []matchupDifficultyPlayer,
	name string,
) (matchupDifficultyPlayer, bool) {
	for _, player := range players {
		if player.Name == name {
			return player, true
		}
	}
	return matchupDifficultyPlayer{}, false
}

type matchupDifficultyIntegrationProvider struct {
	data        fantasy.LeagueData
	table       fantasy.DefenseVsPositionTable
	ratingCalls int
}

func (provider *matchupDifficultyIntegrationProvider) DescribeLeague(
	_ context.Context,
	leagueID string,
) (fantasy.League, error) {
	league := provider.data.League
	league.Ref.ExternalID = leagueID
	return league, nil
}

func (provider *matchupDifficultyIntegrationProvider) FetchLeague(
	_ context.Context,
	league fantasy.League,
	_ fantasy.FetchRequest,
) (fantasy.LeagueData, error) {
	data := provider.data
	data.League = league
	return data, nil
}

func (provider *matchupDifficultyIntegrationProvider) DefenseVsPosition(
	context.Context,
	fantasy.League,
) (fantasy.DefenseVsPositionTable, error) {
	provider.ratingCalls++
	return provider.table, nil
}

type matchupDifficultyIntegrationSources struct {
	provider fantasy.Provider
}

func (sources matchupDifficultyIntegrationSources) SourceFor(leaguesync.Target) (fantasy.Provider, error) {
	return sources.provider, nil
}
