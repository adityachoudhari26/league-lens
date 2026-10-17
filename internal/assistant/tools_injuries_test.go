package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type injurySourceStub struct {
	injuries map[int64]*espn.AthleteInjury
	err      error
	season   int
	calls    [][]int64
}

func (s *injurySourceStub) AthleteInjuries(_ context.Context, season int, athleteIDs []int64) (map[int64]*espn.AthleteInjury, error) {
	s.season = season
	s.calls = append(s.calls, athleteIDs)
	return s.injuries, s.err
}

func TestInjuryToolIsRegisteredWithLabelAndDescription(t *testing.T) {
	service := &Service{Injuries: &injurySourceStub{}}
	tools := service.chatTools()
	tool := findAssistantToolByName(tools, "get_injury_report")
	if tool == nil || tool.label(json.RawMessage(`{}`)) != "Checking injury reports" {
		t.Fatal("injury tool is missing or has the wrong label")
	}
	if tool.definition.Description != getInjuryReportDescription ||
		!strings.HasPrefix(tool.definition.Description, "Current injury status, injury, expected return date") {
		t.Fatalf("tool description = %q", tool.definition.Description)
	}
	if findAssistantToolByName((&Service{}).chatTools(), "get_injury_report") != nil {
		t.Fatal("injury tool registered without an injury source")
	}
}

func TestInjuryToolRejectsInvalidInputBeforeLoading(t *testing.T) {
	source := &injurySourceStub{}
	tools := (&Service{Injuries: source}).chatTools()
	elevenIDs := `"` + strings.Repeat(`00000000-0000-0000-0000-000000000001","`, 10) + `00000000-0000-0000-0000-000000000001"`
	cases := map[string]string{
		`{}`:                                 "pass league_season_id or player_ids",
		`{"player_ids":[]}`:                  "pass league_season_id or player_ids",
		`{"league_season_id":"x","week":5}`:  "invalid tool input",
		`{"league_season_id":"not-a-uuid"}`:  "invalid tool input",
		`{"player_ids":["not-a-uuid"]}`:      "invalid tool input",
		`{"player_ids":[` + elevenIDs + `]}`: "pass at most 10 player_ids",
	}
	for input, want := range cases {
		result := runAssistantTool(context.Background(), tools, llm.ToolCall{
			ID: "injuries", Name: "get_injury_report", Input: json.RawMessage(input),
		})
		if !result.IsError || result.Content != want {
			t.Errorf("input %s: result = %+v, want %q", input, result, want)
		}
	}
	if len(source.calls) != 0 {
		t.Fatalf("injury source called for invalid input: %v", source.calls)
	}
}

func TestBuildInjuryReportKeepsCurrentInjuriesAndRequestedHealthyPlayers(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-48 * time.Hour)
	stale := now.Add(-22 * 24 * time.Hour)
	players := []injuryPlayer{
		{name: "Recent Questionable", nflTeam: "KC", athleteID: 1},
		{name: "Stale Questionable", nflTeam: "BUF", athleteID: 2},
		{name: "Stale IR", nflTeam: "SF", athleteID: 3},
		{name: "Stale Out", nflTeam: "DAL", athleteID: 4, requested: true},
		{name: "Healthy Roster", nflTeam: "MIA", athleteID: 5},
		{name: "Healthy Requested", nflTeam: "WAS", athleteID: 6, requested: true},
		{name: "Stale Requested", nflTeam: "JAX", athleteID: 2, requested: true},
		{name: "Unmapped Requested", nflTeam: "DET", requested: true},
		{name: "Lookup Failed", nflTeam: "GB", athleteID: 7, requested: true},
	}
	injuries := map[int64]*espn.AthleteInjury{
		1: {Status: "Questionable", Injury: "Hamstring", ReturnDate: "2026-10-18", Note: "Limited Thursday.", Updated: recent},
		2: {Status: "Questionable", Injury: "Ankle", Updated: stale},
		3: {Status: "Injured Reserve", Injury: "Knee", Updated: stale},
		4: {Status: "Out", Updated: stale},
		5: nil,
		6: {Status: "Active", Updated: recent},
	}

	report, err := json.Marshal(buildInjuryReport(players, injuries, now))
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"players":[` +
		`{"name":"Recent Questionable","nfl_team":"KC","status":"Questionable","injury":"Hamstring",` +
		`"return_date":"2026-10-18","note":"Limited Thursday.","updated":"2026-10-14T12:00:00Z"},` +
		`{"name":"Stale IR","nfl_team":"SF","status":"IR","injury":"Knee","updated":"2026-09-24T12:00:00Z"},` +
		`{"name":"Stale Out","nfl_team":"DAL","status":"Out","updated":"2026-09-24T12:00:00Z"},` +
		`{"name":"Healthy Requested","nfl_team":"WAS","status":"Active"},` +
		`{"name":"Stale Requested","nfl_team":"JAX","status":"Active"}]}`
	if string(report) != expected {
		t.Fatalf("report = %s\nwant %s", report, expected)
	}
}

func TestEspnAthleteIDSkipsTeamDefensesAndBadIDs(t *testing.T) {
	for value, want := range map[string]int64{"3915411": 3915411, "-16034": 0, "": 0, "abc": 0} {
		if got := espnAthleteID(value); got != want {
			t.Errorf("espnAthleteID(%q) = %d, want %d", value, got, want)
		}
	}
}
