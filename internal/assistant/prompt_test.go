package assistant

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func TestBuildSystemPromptDateAndPageContext(t *testing.T) {
	today := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	week := int32(7)
	prompt := buildSystemPrompt(today, &v1.PageContext{
		Path: "/matchups", LeagueSeasonId: "league-1", TeamId: "team-2", Week: &week,
	})
	for _, expected := range []string{
		"You are the League Lens assistant, built into a fantasy football dashboard. The user owns teams in one or more ESPN and Sleeper leagues.",
		"Today is Sunday, October 18, 2026. The user is looking at Matchups (/matchups), league league-1, team team-2, week 7.",
		"- Answer from League Lens data returned by your tools. Never invent scores, " +
			"projections, injuries, win chances, or news. If the user asks for something your tools don't return, say so in one short sentence.",
		"- You can't make changes on ESPN or Sleeper.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestBuildSystemPromptPageVariants(t *testing.T) {
	today := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	week := int32(3)
	tests := []struct {
		name string
		page *v1.PageContext
		want string
	}{
		{name: "empty page", page: &v1.PageContext{}, want: "Today is Friday, January 2, 2026.\n\nRules:"},
		{name: "home", page: &v1.PageContext{Path: "/"}, want: "The user is looking at Home (/)."},
		{name: "roster", page: &v1.PageContext{Path: "/roster"}, want: "The user is looking at Roster (/roster)."},
		{name: "matchups", page: &v1.PageContext{Path: "/matchups"}, want: "The user is looking at Matchups (/matchups)."},
		{name: "standings", page: &v1.PageContext{Path: "/standings"}, want: "The user is looking at Standings (/standings)."},
		{name: "settings", page: &v1.PageContext{Path: "/settings"}, want: "The user is looking at Settings (/settings)."},
		{name: "unknown", page: &v1.PageContext{Path: "/connect"}, want: "The user is looking at (/connect)."},
		{name: "optional week zero", page: &v1.PageContext{Path: "/", Week: &week}, want: "Home (/), week 3."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prompt := buildSystemPrompt(today, test.page)
			if !strings.Contains(prompt, test.want) {
				t.Fatalf("prompt missing %q:\n%s", test.want, prompt)
			}
		})
	}
}

func TestBuildSystemPromptIncludesLineupUsageGuidance(t *testing.T) {
	prompt := buildSystemPrompt(time.Now(), nil)
	for _, expected := range []string{
		"- Start/sit, waiver, and trade suggestions are opinions. Give your pick and the reasons behind it.",
		"- For lineup and start/sit questions, check each close call (a starter and a bench option within about 3 projected points, or a " +
			"questionable starter) with get_player_usage, get_matchup_difficulty and get_game_context before you decide. Judge a player's " +
			"role by his most recent games, not only the average: position_room shows when a same-position teammate returned or missed games.",
		"- In lineup answers, give each change you suggest its own short entry: the two projections, then the usage, matchup or " +
			"game-context points for and against it. Mention close calls you kept as they are in one line each.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q:\n%s", expected, prompt)
		}
	}
	if strings.Contains(prompt, "one or two reasons") {
		t.Fatalf("outdated start/sit guidance remains in prompt:\n%s", prompt)
	}
}

func TestBuildSystemPromptIncludesFreeAgentAdvice(t *testing.T) {
	prompt := buildSystemPrompt(time.Now(), nil)
	waiverAdvice := "- For waiver advice, call get_free_agents and compare against the user's roster. " +
		"Say if a player is on waivers rather than a free agent."
	if !strings.Contains(prompt, waiverAdvice) {
		t.Fatalf("free-agent guidance missing from prompt:\n%s", prompt)
	}
	tradeAdvice := "- For trade questions, find candidate players with get_league and get_team_roster, then call evaluate_trade. " +
		"Weigh recent points, projections, injuries, byes, and roster needs."
	if !strings.Contains(prompt, tradeAdvice) {
		t.Fatalf("trade guidance missing from prompt:\n%s", prompt)
	}
	missingDataRule := "- Don't list data you don't have or add caveats about missing factors " +
		"(weather, betting lines, practice reports, and so on) unless the user asks about them."
	if !strings.Contains(prompt, missingDataRule) {
		t.Fatalf("missing-data guidance missing from prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "don't capture") {
		t.Fatalf("trade guidance still lists missing data:\n%s", prompt)
	}
}
