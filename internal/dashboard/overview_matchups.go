package dashboard

import (
	"context"
	"errors"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) GetOverviewMatchups(ctx context.Context, _ *v1.GetOverviewMatchupsRequest) (*v1.GetOverviewMatchupsResponse, error) {
	response := &v1.GetOverviewMatchupsResponse{Coverage: coverage("empty", "Import a league to see your matchups.")}
	err := s.readOverview(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID, teams []store.OverviewTeamsRow) error {
		if len(teams) == 0 {
			return nil
		}
		response.Coverage = coverage("available", "Scores from your latest import, not a live feed. Showing the latest imported season.")
		for _, team := range teams {
			item, err := readOverviewMatchup(ctx, q, owner, team)
			if err != nil {
				return err
			}
			if item.Status == "unavailable" {
				response.Coverage = coverage("partial", "Some matchups are unavailable. Scores reflect the last import, not a live feed.")
			}
			response.Matchups = append(response.Matchups, item)
		}
		return nil
	})
	return response, err
}
func readOverviewMatchup(ctx context.Context, q *store.Queries, owner pgtype.UUID, team store.OverviewTeamsRow) (*v1.OverviewMatchup, error) {
	item := &v1.OverviewMatchup{Team: overviewTeam(team), Status: "unavailable"}
	if team.Period == 0 {
		return item, nil
	}
	row, err := q.OverviewTeamMatchup(ctx, store.OverviewTeamMatchupParams{TeamAID: team.ID, UserID: owner, Column3: team.Period})
	if errors.Is(err, pgx.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return nil, err
	}
	score, opponentScore := row.ScoreA, row.ScoreB
	item.OpponentName = row.TeamBName.String
	if row.TeamBID == team.ID {
		score, opponentScore = opponentScore, score
		item.OpponentName = row.TeamAName
	}
	item.Score, err = optionalScore(score)
	if err != nil {
		return nil, err
	}
	item.OpponentScore, err = optionalScore(opponentScore)
	if err != nil {
		return nil, err
	}
	item.MatchupPeriod = &row.Period
	item.Status = row.Status
	item.IsBye = row.IsBye
	item.FetchedAt = timestamp(row.FetchedAt)
	return item, nil
}
func optionalScore(score pgtype.Numeric) (*float64, error) {
	if !score.Valid {
		return nil, nil
	}
	value, err := score.Float64Value()
	if err != nil {
		return nil, err
	}
	return &value.Float64, nil
}
