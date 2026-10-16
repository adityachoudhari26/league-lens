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
	}, false)
	for _, expected := range []string{
		"You are the League Lens assistant, built into a fantasy football dashboard. The user owns teams in one or more ESPN and Sleeper leagues.",
		"Today is Sunday, October 18, 2026. The user is looking at Matchups (/matchups), league league-1, team team-2, week 7.",
		"- Answer from League Lens data returned by your tools. Never invent scores, " +
			"projections, injuries, win chances, or news. If the data doesn't have it, say so.",
		"- You can't make changes on ESPN or Sleeper.",
		noToolsPromptRule,
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
			prompt := buildSystemPrompt(today, test.page, false)
			if !strings.Contains(prompt, test.want) {
				t.Fatalf("prompt missing %q:\n%s", test.want, prompt)
			}
		})
	}
}

func TestBuildSystemPromptOmitsNoToolsRuleWhenToolsExist(t *testing.T) {
	prompt := buildSystemPrompt(time.Now(), nil, true)
	if strings.Contains(prompt, noToolsPromptRule) {
		t.Fatal("tools prompt contains no-tools rule")
	}
}
