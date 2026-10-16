package assistant

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

const noToolsPromptRule = "- You don't have access to league data in this version. " +
	"If asked about the user's leagues, say that league data isn't connected to the assistant yet."

// buildSystemPrompt adds the current date, available page context, and tool limits.
func buildSystemPrompt(today time.Time, page *v1.PageContext, toolsExist bool) string {
	introduction := "Today is " + today.Format("Monday, January 2, 2006") + "."
	if pageLine := pageContextLine(page); pageLine != "" {
		introduction += " " + pageLine
	}
	prompt := "You are Playbook, the League Lens assistant built into a fantasy football dashboard. " +
		"The user owns teams in one or more ESPN and Sleeper leagues.\n" +
		introduction + "\n\n" +
		"Rules:\n" +
		"- Answer from League Lens data returned by your tools. Never invent scores, " +
		"projections, injuries, win chances, or news. If the data doesn't have it, say so.\n" +
		"- Live scores are what has happened. Projections and win chance are predictions. Keep them apart and say which one you mean.\n" +
		"- When you give a number, say where it came from in a few words, like \"projection, week 7\".\n" +
		"- Be brief: a few sentences or a short list. Plain text only: no headings, tables, or bold.\n" +
		"- Start/sit, waiver, and trade suggestions are opinions. Give your pick and the one or two reasons behind it.\n" +
		"- You can't make changes on ESPN or Sleeper."
	if !toolsExist {
		prompt += "\n" + noToolsPromptRule
	}
	return prompt
}

func pageContextLine(page *v1.PageContext) string {
	if page == nil || page.GetPath() == "" {
		return ""
	}
	name := pageName(page.GetPath())
	line := "The user is looking at "
	if name != "" {
		line += name + " "
	}
	line += fmt.Sprintf("(%s)", page.GetPath())
	context := make([]string, 0, 3)
	if page.GetLeagueSeasonId() != "" {
		context = append(context, "league "+page.GetLeagueSeasonId())
	}
	if page.GetTeamId() != "" {
		context = append(context, "team "+page.GetTeamId())
	}
	if page.Week != nil {
		context = append(context, fmt.Sprintf("week %d", page.GetWeek()))
	}
	if len(context) > 0 {
		line += ", " + strings.Join(context, ", ")
	}
	return line + "."
}

func pageName(path string) string {
	switch path {
	case "/":
		return "Home"
	case "/roster":
		return "Roster"
	case "/matchups":
		return "Matchups"
	case "/standings":
		return "Standings"
	case "/settings":
		return "Settings"
	default:
		return ""
	}
}
