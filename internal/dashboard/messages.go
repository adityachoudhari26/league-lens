package dashboard

import (
	"encoding/json"
	"slices"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func timestamp(value pgtype.Timestamptz) *timestamppb.Timestamp {
	if !value.Valid {
		return nil
	}
	return timestamppb.New(value.Time)
}

func leagueMessage(row store.DashboardLeagueRow) *v1.League {
	result := &v1.League{Id: row.ID.String(), Provider: row.Provider, ExternalId: row.ExternalLeagueID, Season: row.Season, Name: row.Name, LastSuccessfulSyncAt: timestamp(row.LastSuccessfulSyncAt)}
	for _, id := range row.SelectedTeamIds {
		result.SelectedTeamIds = append(result.SelectedTeamIds, id.String())
	}
	return result
}

func teamMessage(row store.Team, selected []string) (*v1.Team, error) {
	result := &v1.Team{Id: row.ID.String(), ExternalId: row.ExternalTeamID, Name: row.Name, Selected: slices.Contains(selected, row.ID.String())}
	if row.Rank.Valid {
		result.Rank = &row.Rank.Int32
	}
	if len(row.RecordJson) > 0 {
		var record fantasy.TeamRecord
		if err := json.Unmarshal(row.RecordJson, &record); err != nil {
			return nil, err
		}
		result.Record = &v1.TeamRecord{Wins: int32(record.Wins), Losses: int32(record.Losses), Ties: int32(record.Ties), PointsFor: record.PointsFor, PointsAgainst: record.PointsAgainst}
	}
	return result, nil
}

// rosterMessage preserves snapshot provenance, coverage, and optional position data.
func rosterMessage(row store.TeamSnapshot, entries []store.ListSnapshotRosterForUserRow) (*v1.RosterSnapshot, error) {
	result := &v1.RosterSnapshot{Id: row.ID.String(), TeamId: row.TeamID.String(), ScoringPeriod: row.Period, Origin: row.Origin, Coverage: row.Coverage, ObservedAt: timestamp(row.ObservedAt), EffectiveAt: timestamp(row.EffectiveAt)}
	for _, entry := range entries {
		item := &v1.RosterEntry{PlayerId: entry.PlayerID.String(), PlayerName: entry.DisplayName, PlayerKind: entry.EntityType, Slot: entry.SlotLabel.String, ProviderSlotId: entry.ProviderSlotID.String}
		if len(entry.EligiblePositionsJson) > 0 {
			item.EligiblePositions = &v1.Positions{}
			if err := json.Unmarshal(entry.EligiblePositionsJson, &item.EligiblePositions.Values); err != nil {
				return nil, err
			}
		}
		result.Entries = append(result.Entries, item)
	}
	return result, nil
}

// matchupMessage preserves unknown scores and scoring periods, and explicit bye status.
func matchupMessage(row store.Matchup) (*v1.Matchup, error) {
	a, err := matchupSide(row.TeamAID, row.ScoreA)
	if err != nil {
		return nil, err
	}
	result := &v1.Matchup{Id: row.ID.String(), ExternalId: row.ExternalMatchupID, MatchupPeriod: row.Period, TeamA: a, IsBye: row.IsBye, Status: row.Status, FetchedAt: timestamp(row.FetchedAt)}
	if row.TeamBID.Valid {
		result.TeamB, err = matchupSide(row.TeamBID, row.ScoreB)
		if err != nil {
			return nil, err
		}
	}
	if len(row.ScoringPeriodsJson) > 0 {
		result.ScoringPeriods = &v1.ScoringPeriods{}
		if err = json.Unmarshal(row.ScoringPeriodsJson, &result.ScoringPeriods.Values); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func matchupSide(id pgtype.UUID, score pgtype.Numeric) (*v1.MatchupSide, error) {
	side := &v1.MatchupSide{TeamId: id.String()}
	if score.Valid {
		value, err := score.Float64Value()
		if err != nil {
			return nil, err
		}
		side.Score = &value.Float64
	}
	return side, nil
}
