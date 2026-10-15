package dashboard

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetMatchupDetail returns the owner's selected team's matchup and saved lineup.
func (s *Service) GetMatchupDetail(ctx context.Context, request *v1.GetMatchupDetailRequest) (*v1.GetMatchupDetailResponse, error) {
	leagueSeasonID, err := parseID(request.LeagueSeasonId)
	if err != nil {
		return nil, err
	}
	if request.MatchupPeriod != nil {
		if err = validatePeriod(*request.MatchupPeriod); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	var matchup *v1.MatchupDetail
	err = s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		var err error
		matchup, err = readMatchupDetail(ctx, q, owner, leagueSeasonID, request.MatchupPeriod, now)
		return err
	})
	return &v1.GetMatchupDetailResponse{Matchup: matchup}, err
}

// readMatchupDetail loads the owner's selected team, its round, and the schedule; a missing round is not an error.
func readMatchupDetail(ctx context.Context, q *store.Queries, owner, leagueSeasonID pgtype.UUID, period *int32, now time.Time) (*v1.MatchupDetail, error) {
	selectedTeam, err := q.MatchupDetailOwnerTeam(ctx, store.MatchupDetailOwnerTeamParams{UserID: owner, LeagueSeasonID: leagueSeasonID})
	if err != nil {
		return nil, err
	}

	matchup, found, err := readMatchupRound(ctx, q, owner, leagueSeasonID, selectedTeam, period)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	schedule, err := loadMatchupSchedule(ctx, q, selectedTeam.Season, matchup.ScoringPeriodsJson)
	if err != nil {
		return nil, err
	}

	return matchupDetailMessage(ctx, q, owner, selectedTeam.TeamID, matchup, schedule, now)
}

// readMatchupRound returns false when no observed week or matching round is available.
func readMatchupRound(ctx context.Context, q *store.Queries, owner, leagueSeasonID pgtype.UUID, selectedTeam store.MatchupDetailOwnerTeamRow, period *int32) (store.Matchup, bool, error) {
	if period == nil && selectedTeam.ObservedPeriod == 0 {
		return store.Matchup{}, false, nil
	}

	matchupPeriod := int32(0)
	if period != nil {
		matchupPeriod = *period
	}
	matchup, err := q.MatchupDetailRound(ctx, store.MatchupDetailRoundParams{
		LeagueSeasonID: leagueSeasonID,
		TeamID:         selectedTeam.TeamID,
		Period:         matchupPeriod,
		ObservedPeriod: selectedTeam.ObservedPeriod,
		UserID:         owner,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Matchup{}, false, nil
	}
	if err != nil {
		return store.Matchup{}, false, err
	}
	return matchup, true, nil
}

// loadMatchupSchedule loads the schedule only when the matchup spans exactly one scoring week.
func loadMatchupSchedule(ctx context.Context, q *store.Queries, season int32, scoringPeriodsJSON []byte) (weekSchedule, error) {
	scoringWeek, hasSingleWeek, err := singleScoringWeek(scoringPeriodsJSON)
	if err != nil {
		return weekSchedule{}, err
	}
	if !hasSingleWeek {
		return weekSchedule{}, nil
	}
	return (weekSchedules{}).load(ctx, q, season, scoringWeek)
}

// matchupDetailMessage orients the owner first, swaps scores when the owner is team B, and omits an absent opponent.
func matchupDetailMessage(ctx context.Context, q *store.Queries, owner, selectedTeamID pgtype.UUID, matchup store.Matchup, schedule weekSchedule, now time.Time) (*v1.MatchupDetail, error) {
	teamScore, opponentScore := matchup.ScoreA, matchup.ScoreB
	opponentID := matchup.TeamBID
	if matchup.TeamBID == selectedTeamID {
		teamScore, opponentScore = opponentScore, teamScore
		opponentID = matchup.TeamAID
	}

	team, err := matchupLineupMessage(ctx, q, owner, selectedTeamID, teamScore, matchup.ID, schedule, now)
	if err != nil {
		return nil, err
	}
	detail := &v1.MatchupDetail{
		Id:            matchup.ID.String(),
		MatchupPeriod: matchup.Period,
		Status:        matchupStatusMessage(matchup.Status),
		IsBye:         matchup.IsBye,
		Team:          team,
		FetchedAt:     timestamp(matchup.FetchedAt),
	}
	if !matchup.IsBye && opponentID.Valid {
		detail.Opponent, err = matchupLineupMessage(ctx, q, owner, opponentID, opponentScore, matchup.ID, schedule, now)
		if err != nil {
			return nil, err
		}
	}
	return detail, nil
}

// matchupLineupMessage maps a team's saved starters and bench, including IR and taxi entries; an empty lineup still has its name and score.
func matchupLineupMessage(ctx context.Context, q *store.Queries, owner, teamID pgtype.UUID, score pgtype.Numeric, matchupID pgtype.UUID, schedule weekSchedule, now time.Time) (*v1.MatchupLineup, error) {
	team, err := q.DashboardTeam(ctx, store.DashboardTeamParams{ID: teamID, UserID: owner})
	if err != nil {
		return nil, err
	}
	record, err := teamRecordMessage(team.RecordJson)
	if err != nil {
		return nil, err
	}
	lineup := &v1.MatchupLineup{
		TeamId:   team.ID.String(),
		TeamName: team.Name,
		Record:   record,
	}
	lineup.Score, err = optionalScore(score)
	if err != nil {
		return nil, err
	}

	rows, err := q.MatchupDetailLineup(ctx, store.MatchupDetailLineupParams{MatchupID: matchupID, TeamID: teamID, UserID: owner})
	if err != nil {
		return nil, err
	}
	lineup.Starters, lineup.Bench, err = lineupPlayers(rows, schedule, now)
	if err != nil {
		return nil, err
	}
	sortLineupPlayers(lineup.Starters, starterSlotOrder)
	sortLineupPlayers(lineup.Bench, benchSlotOrder)
	lineup.StartersLeftToPlay = startersLeftToPlay(lineup.Starters)
	lineup.ProjectedScore = projectedScore(lineup.Starters)
	return lineup, nil
}

// lineupPlayers maps saved rows and splits them into starters and bench (bench includes IR and taxi).
func lineupPlayers(rows []store.MatchupDetailLineupRow, schedule weekSchedule, now time.Time) (starters, bench []*v1.LineupPlayer, err error) {
	for _, row := range rows {
		player, err := lineupPlayerMessage(row, schedule, now)
		if err != nil {
			return nil, nil, err
		}
		if row.Starter {
			starters = append(starters, player)
		} else {
			bench = append(bench, player)
		}
	}
	return starters, bench, nil
}

// lineupPlayerMessage maps a saved player with current injury and NFL team metadata from the latest observed snapshot; dropped players have neither.
func lineupPlayerMessage(row store.MatchupDetailLineupRow, schedule weekSchedule, now time.Time) (*v1.LineupPlayer, error) {
	points, err := optionalScore(row.Points)
	if err != nil {
		return nil, err
	}
	projectedPoints, err := optionalScore(row.ProjectedPoints)
	if err != nil {
		return nil, err
	}
	gameState, kickoffAt := lineupGameState(row.ProTeam.String, schedule, now)
	return &v1.LineupPlayer{
		PlayerId:        row.PlayerID.String(),
		PlayerName:      row.DisplayName,
		Slot:            row.SlotLabel.String,
		Points:          points,
		ProjectedPoints: projectedPoints,
		InjuryStatus:    injuryStatusMessage(fantasy.InjuryStatus(row.InjuryStatus.String)),
		ProTeam:         row.ProTeam.String,
		GameState:       gameState,
		KickoffAt:       kickoffAt,
	}, nil
}

// singleScoringWeek returns a week only when a matchup spans exactly one known week.
func singleScoringWeek(raw []byte) (int32, bool, error) {
	if len(raw) == 0 {
		return 0, false, nil
	}
	var weeks []int32
	if err := json.Unmarshal(raw, &weeks); err != nil {
		return 0, false, err
	}
	if len(weeks) != 1 {
		return 0, false, nil
	}
	return weeks[0], true, nil
}

func matchupStatusMessage(status string) v1.MatchupStatus {
	switch fantasy.MatchupStatus(status) {
	case fantasy.MatchupScheduled:
		return v1.MatchupStatus_MATCHUP_STATUS_SCHEDULED
	case fantasy.MatchupInProgress:
		return v1.MatchupStatus_MATCHUP_STATUS_IN_PROGRESS
	case fantasy.MatchupFinal:
		return v1.MatchupStatus_MATCHUP_STATUS_FINAL
	default:
		return v1.MatchupStatus_MATCHUP_STATUS_UNKNOWN
	}
}

// lineupGameState derives kickoff state only from a loaded schedule and known NFL team.
func lineupGameState(proTeam string, schedule weekSchedule, now time.Time) (v1.GameState, *timestamppb.Timestamp) {
	if proTeam == "" || !schedule.loaded() {
		return v1.GameState_GAME_STATE_UNKNOWN, nil
	}
	game, hasGame := schedule.games[proTeam]
	if !hasGame {
		return v1.GameState_GAME_STATE_BYE, nil
	}
	kickoffAt := timestamp(game.KickoffAt)
	if game.KickoffAt.Time.After(now) {
		return v1.GameState_GAME_STATE_NOT_STARTED, kickoffAt
	}
	return v1.GameState_GAME_STATE_KICKED_OFF, kickoffAt
}

func startersLeftToPlay(starters []*v1.LineupPlayer) int32 {
	var remaining int32
	for _, starter := range starters {
		if starter.GameState == v1.GameState_GAME_STATE_NOT_STARTED {
			remaining++
		}
	}
	return remaining
}

// projectedScore is absent when no starter has a projection.
func projectedScore(starters []*v1.LineupPlayer) *float64 {
	var total float64
	hasProjection := false
	for _, starter := range starters {
		hasProjection = hasProjection || starter.ProjectedPoints != nil
		total += starterProjectedPoints(starter)
	}
	if !hasProjection {
		return nil
	}
	total = math.Round(total*10000) / 10000 // Stored points have four decimals; drop float sum noise.
	return &total
}

// starterProjectedPoints counts the projection only before kickoff; bye and
// unknown-schedule starters keep their actual points. Missing counts as zero.
func starterProjectedPoints(starter *v1.LineupPlayer) float64 {
	points := starter.Points
	if starter.GameState == v1.GameState_GAME_STATE_NOT_STARTED {
		points = starter.ProjectedPoints
	}
	if points == nil {
		return 0
	}
	return *points
}

var starterSlotOrder = []fantasy.RosterSlot{
	fantasy.SlotQB, fantasy.SlotRB, fantasy.SlotWR, fantasy.SlotTE,
	fantasy.SlotFlex, fantasy.SlotSuperflex, fantasy.SlotDefense, fantasy.SlotK,
}

var benchSlotOrder = []fantasy.RosterSlot{fantasy.SlotBench, fantasy.SlotTaxi, fantasy.SlotIR}

// sortLineupPlayers orders by slot, then actual points and stable player identity.
func sortLineupPlayers(players []*v1.LineupPlayer, slotOrder []fantasy.RosterSlot) {
	ranks := make(map[string]int, len(slotOrder))
	for rank, slot := range slotOrder {
		ranks[string(slot)] = rank
	}
	slotRank := func(slot string) int {
		if rank, ok := ranks[slot]; ok {
			return rank
		}
		return len(slotOrder)
	}
	slices.SortFunc(players, func(a, b *v1.LineupPlayer) int {
		return cmp.Or(
			cmp.Compare(slotRank(a.Slot), slotRank(b.Slot)),
			comparePointsDesc(a.Points, b.Points),
			cmp.Compare(a.PlayerName, b.PlayerName),
			cmp.Compare(a.PlayerId, b.PlayerId),
		)
	})
}

func comparePointsDesc(a, b *float64) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return 1
	}
	if b == nil {
		return -1
	}
	return cmp.Compare(*b, *a)
}
