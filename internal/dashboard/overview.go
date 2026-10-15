package dashboard

import (
	"context"
	"fmt"
	"net/url"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5/pgtype"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// readOverview bounds reads to 100 selected teams in the latest imported season.
// No provider network calls run in these read-only transactions.
func (s *Service) readOverview(ctx context.Context, fn func(context.Context, *store.Queries, pgtype.UUID, []store.OverviewTeamsRow) error) error {
	err := s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		teams, err := q.OverviewTeams(ctx, owner)
		if err != nil {
			return err
		}
		if len(teams) > 100 {
			return fmt.Errorf("overview exceeds selected team limit")
		}
		return fn(ctx, q, owner, teams)
	})
	if connect.CodeOf(err) == connect.CodeNotFound {
		return nil
	}
	return err
}

func overviewTeam(row store.OverviewTeamsRow) *v1.OverviewTeam {
	team := &v1.OverviewTeam{
		Id:         row.ID.String(),
		Name:       row.Name,
		LeagueId:   row.LeagueID.String(),
		LeagueName: row.LeagueName,
		LeagueUrl:  providerLeagueURL(row.Provider, row.ExternalLeagueID, row.Season),
		Provider:   row.Provider,
		Season:     row.Season,
		ObservedAt: timestamp(row.ObservedAt),
	}
	if row.Period > 0 {
		team.ScoringPeriod = &row.Period
	}
	return team
}

// providerLeagueURL returns the provider's league home page, built with net/url so
// IDs are escaped; unknown providers get "" so the UI shows plain text.
func providerLeagueURL(provider, externalLeagueID string, season int32) string {
	switch provider {
	case "espn":
		return (&url.URL{
			Scheme: "https",
			Host:   "fantasy.espn.com",
			Path:   "/football/league",
			RawQuery: url.Values{
				"leagueId": {externalLeagueID},
				"seasonId": {fmt.Sprint(season)},
			}.Encode(),
		}).String()
	case "sleeper":
		escapedID := url.PathEscape(externalLeagueID)
		return (&url.URL{
			Scheme:  "https",
			Host:    "sleeper.com",
			Path:    "/leagues/" + externalLeagueID,
			RawPath: "/leagues/" + escapedID,
		}).String()
	default:
		return ""
	}
}

func coverage(state, explanation string) *v1.OverviewCoverage {
	return &v1.OverviewCoverage{State: state, Explanation: explanation}
}
