package espn

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.Provider = (*Client)(nil)

func (c *Client) DescribeLeague(ctx context.Context, id string) (fantasy.League, error) {
	raw, err := c.readLeague(ctx, id, 0)
	if err != nil {
		return fantasy.League{}, err
	}
	return normalizeLeague(raw)
}

func (c *Client) FetchLeague(ctx context.Context, league fantasy.League) (fantasy.LeagueData, error) {
	raw, err := c.readLeague(ctx, league.Ref.ExternalID, 0)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	described, err := normalizeLeague(raw)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	if described.Ref != league.Ref {
		return fantasy.LeagueData{}, fmt.Errorf("ESPN league identity changed")
	}
	if raw.Week < 1 || raw.Week > 18 || raw.Status.LatestWeek != raw.Week {
		return fantasy.LeagueData{}, fmt.Errorf("unsupported ESPN current scoring week")
	}
	data := fantasy.LeagueData{League: described, FetchedAt: time.Now().UTC()}
	players := map[string]fantasy.Player{}
	for _, team := range raw.Teams {
		data.Teams = append(data.Teams, normalizeTeam(team))
	}
	if err = appendRosters(&data, players, raw, fantasy.SnapshotObserved); err != nil {
		return fantasy.LeagueData{}, err
	}
	if err = appendMatchups(&data, raw); err != nil {
		return fantasy.LeagueData{}, err
	}
	if err = appendLineups(&data, players, raw); err != nil {
		return fantasy.LeagueData{}, err
	}
	if err = c.appendHistory(ctx, &data, players, raw); err != nil {
		return fantasy.LeagueData{}, err
	}
	data.Players = sortedPlayers(players)
	return data, nil
}

func (c *Client) appendHistory(ctx context.Context, data *fantasy.LeagueData, players map[string]fantasy.Player, raw leagueResponse) error {
	for week := 1; week < raw.Week; week++ {
		history, fetchErr := c.readLeague(ctx, data.League.Ref.ExternalID, week)
		if fetchErr != nil {
			return fetchErr
		}
		if err := validateTeams(history, raw); err != nil {
			return err
		}
		if err := appendRosters(data, players, history, fantasy.SnapshotHistory); err != nil {
			return err
		}
		if err := appendLineups(data, players, history); err != nil {
			return err
		}
	}
	return nil
}

func sortedPlayers(players map[string]fantasy.Player) []fantasy.Player {
	var result []fantasy.Player
	ids := make([]string, 0, len(players))
	for id := range players {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		result = append(result, players[id])
	}
	return result
}
func normalizeLeague(raw leagueResponse) (fantasy.League, error) {
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: strconv.FormatInt(raw.ID, 10), Season: raw.Season}, Name: raw.Settings.Name, TeamCount: raw.Settings.Size}
	if league.Name == "" || league.TeamCount < 1 || league.TeamCount > 100 {
		return league, fmt.Errorf("invalid ESPN league settings")
	}
	if err := validateTeams(raw, raw); err != nil {
		return league, err
	}
	settings, err := normalizeSettings(raw.Settings)
	league.Settings = settings
	return league, err
}
func validateTeams(raw, current leagueResponse) error {
	if len(raw.Teams) != current.Settings.Size {
		return fmt.Errorf("incomplete ESPN team list")
	}
	expected := map[int]bool{}
	for _, team := range current.Teams {
		expected[team.ID] = true
	}
	seen := map[int]bool{}
	for _, team := range raw.Teams {
		if team.ID <= 0 || seen[team.ID] || !expected[team.ID] {
			return fmt.Errorf("invalid ESPN team list")
		}
		seen[team.ID] = true
	}
	return nil
}
func normalizeTeam(raw teamResponse) fantasy.Team {
	name := raw.Name
	if name == "" {
		name = fmt.Sprintf("Team %d", raw.ID)
	}
	return fantasy.Team{ExternalID: strconv.Itoa(raw.ID), Name: name, Record: normalizeRecord(raw)}
}
func normalizeRecord(raw teamResponse) *fantasy.TeamRecord {
	if raw.Record == nil || raw.Record.Overall == nil {
		return nil
	}
	record := raw.Record.Overall
	if record.Wins == nil || record.Losses == nil || record.Ties == nil {
		return nil
	}
	return &fantasy.TeamRecord{Wins: *record.Wins, Losses: *record.Losses, Ties: *record.Ties, PointsFor: record.PointsFor, PointsAgainst: record.PointsAgainst}
}
