package sleeper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func normalizeLeague(raw League) (fantasy.League, error) {
	year, err := strconv.Atoi(raw.Season)
	if err != nil {
		return fantasy.League{}, err
	}
	settings, err := normalizeSettings(raw)
	if err != nil {
		return fantasy.League{}, err
	}
	return fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: raw.ID, Season: year}, Name: raw.Name, TeamCount: raw.TotalRosters, Settings: settings}, nil
}
func normalizeData(raw Data) (fantasy.LeagueData, error) {
	league, err := normalizeLeague(raw.League)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	data := fantasy.LeagueData{League: league, FetchedAt: raw.ObservedAt}
	names := map[string]string{}
	for _, user := range raw.Users {
		name := user.Metadata["team_name"]
		if name == "" {
			name = user.DisplayName
		}
		names[user.ID] = name
	}
	for _, roster := range raw.Rosters {
		team, err := normalizeTeam(roster, names[roster.OwnerID])
		if err != nil {
			return data, err
		}
		data.Teams = append(data.Teams, team)
		if roster.Players == nil && roster.Starters == nil {
			continue
		}
		snapshot, err := normalizeRoster(roster, raw.League.Positions)
		if err != nil {
			return data, err
		}
		snapshot.ScoringPeriod = fantasy.ScoringPeriod(raw.Period)
		snapshot.Origin = fantasy.SnapshotObserved
		snapshot.ObservedAt = raw.ObservedAt
		snapshot.Record = team.Record
		data.Rosters = append(data.Rosters, snapshot)
	}
	for week := 1; week <= raw.Period; week++ {
		rows := raw.Weeks[week]
		if len(rows) == 0 {
			continue
		}
		historical := week < raw.Period || raw.State.SeasonType == "post"
		if historical {
			for _, row := range rows {
				if row.Players == nil && row.Starters == nil {
					continue
				}
				snapshot, err := normalizeRoster(Roster{ID: row.RosterID, Players: row.Players, Starters: row.Starters}, raw.League.Positions)
				if err != nil {
					return data, err
				}
				snapshot.ScoringPeriod = fantasy.ScoringPeriod(week)
				snapshot.Origin = fantasy.SnapshotHistory
				snapshot.ObservedAt = raw.ObservedAt
				data.Rosters = append(data.Rosters, snapshot)
			}
		}
		matchups, err := normalizeMatchups(rows, week, historical)
		if err != nil {
			return data, err
		}
		data.Matchups = append(data.Matchups, matchups...)
		data.CompleteMatchupPeriods = append(data.CompleteMatchupPeriods, fantasy.MatchupPeriod(week))
	}
	data.Players = encounteredPlayers(data.Rosters)
	return data, nil
}
func normalizeTeam(roster Roster, name string) (fantasy.Team, error) {
	if name == "" {
		name = fmt.Sprintf("Team %d", roster.ID)
	}
	team := fantasy.Team{ExternalID: strconv.Itoa(roster.ID), Name: name}
	if roster.Settings == nil {
		return team, nil
	}
	for _, key := range []string{"wins", "losses", "ties"} {
		value, known := roster.Settings[key]
		if !known || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return team, nil
		}
	}
	values, err := json.Marshal(roster.Settings)
	if err != nil {
		return team, err
	}
	// Keep provider field spelling at the adapter boundary.
	var fields struct {
		Wins           int      `json:"wins"`
		Losses         int      `json:"losses"`
		Ties           int      `json:"ties"`
		For            *float64 `json:"fpts"`
		ForDecimal     *float64 `json:"fpts_decimal"`
		Against        *float64 `json:"fpts_against"`
		AgainstDecimal *float64 `json:"fpts_against_decimal"`
	}
	if err = json.Unmarshal(values, &fields); err != nil {
		return team, err
	}
	team.Record = &fantasy.TeamRecord{Wins: fields.Wins, Losses: fields.Losses, Ties: fields.Ties, PointsFor: combinedPoints(fields.For, fields.ForDecimal), PointsAgainst: combinedPoints(fields.Against, fields.AgainstDecimal)}
	return team, nil
}
func combinedPoints(whole, decimal *float64) *float64 {
	if whole == nil {
		return nil
	}
	points := *whole
	if decimal != nil {
		points += *decimal / 100
	}
	return &points
}
func normalizeRoster(roster Roster, positions []string) (fantasy.RosterSnapshot, error) {
	entries, coverage, err := rosterEntries(roster.Players, roster.Starters, roster.Reserve, roster.Taxi, positions)
	snapshot := fantasy.RosterSnapshot{TeamID: strconv.Itoa(roster.ID), Coverage: fantasy.RosterCoverage(coverage)}
	if err != nil {
		return snapshot, err
	}
	snapshot.Entries = make([]fantasy.RosterEntry, 0, len(entries))
	for _, entry := range entries {
		slot, _ := normalizeSlot(entry.Label)
		snapshot.Entries = append(snapshot.Entries, fantasy.RosterEntry{PlayerID: entry.ID, ProviderSlotID: entry.Slot, Slot: slot})
	}
	return snapshot, nil
}
func encounteredPlayers(rosters []fantasy.RosterSnapshot) []fantasy.Player {
	byID := map[string]fantasy.Player{}
	defenses := " ARI ATL BAL BUF CAR CHI CIN CLE DAL DEN DET GB HOU IND JAX KC LAC LAR LV MIA MIN NE NO NYG NYJ PHI PIT SEA SF TB TEN WAS "
	for _, roster := range rosters {
		for _, entry := range roster.Entries {
			player := fantasy.Player{ExternalID: entry.PlayerID, Name: "Sleeper " + entry.PlayerID, Kind: fantasy.PlayerPerson}
			if strings.Contains(defenses, " "+entry.PlayerID+" ") {
				player.Kind = fantasy.PlayerTeamDefense
				player.Name = entry.PlayerID + " defense"
			}
			byID[entry.PlayerID] = player
		}
	}
	players := make([]fantasy.Player, 0, len(byID))
	for _, player := range byID {
		players = append(players, player)
	}
	sort.Slice(players, func(i, j int) bool { return players[i].ExternalID < players[j].ExternalID })
	return players
}
