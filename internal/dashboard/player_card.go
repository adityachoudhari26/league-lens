package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/playerprofile"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const (
	profileFetchTimeout = 5 * time.Second
	playerCardNewsAge   = 7 * 24 * time.Hour
)

// savedProfile is a player's last saved profile with its stats in source order.
type savedProfile struct {
	profile store.PlayerProfile
	stats   []store.PlayerProfileStat
}

// GetPlayerCard fetches the player's profile live, saves it, and builds the
// card from what is saved. A failed fetch therefore serves the last saved
// profile, or a card with stored League Lens data only.
func (s *Service) GetPlayerCard(ctx context.Context, request *v1.GetPlayerCardRequest) (*v1.GetPlayerCardResponse, error) {
	playerID, err := parseID(request.PlayerId)
	if err != nil {
		return nil, err
	}
	player, err := s.readCardPlayer(ctx, playerID)
	if err != nil {
		return nil, err
	}

	s.refreshProfile(ctx, player)

	card, err := s.readPlayerCard(ctx, player, time.Now())
	if err != nil {
		return nil, err
	}
	return &v1.GetPlayerCardResponse{Card: card}, nil
}

func (s *Service) readCardPlayer(ctx context.Context, playerID pgtype.UUID) (store.PlayerCardPlayerRow, error) {
	var player store.PlayerCardPlayerRow
	err := s.read(ctx, func(ctx context.Context, q *store.Queries, _ pgtype.UUID) error {
		var err error
		player, err = q.PlayerCardPlayer(ctx, playerID)
		return err
	})
	return player, err
}

// refreshProfile replaces the player's saved profile with a live one. Failures
// only log, since the card falls back to the saved profile.
func (s *Service) refreshProfile(ctx context.Context, player store.PlayerCardPlayerRow) {
	if s.profiles == nil {
		return
	}
	profile, err := s.fetchProfile(ctx, player)
	if errors.Is(err, playerprofile.ErrUnknownPlayer) {
		return
	}
	if err != nil {
		slog.Warn("player profile fetch failed; serving the saved profile", "player_id", player.ID.String(), "source", s.profiles.Name(), "error", err)
		return
	}
	if err := s.saveProfile(ctx, player.ID, profile, time.Now()); err != nil {
		slog.Warn("player profile save failed; serving the saved profile", "player_id", player.ID.String(), "error", err)
	}
}

func (s *Service) fetchProfile(ctx context.Context, player store.PlayerCardPlayerRow) (playerprofile.Profile, error) {
	externalIDs := map[string]string{}
	if err := json.Unmarshal(player.ExternalIds, &externalIDs); err != nil {
		return playerprofile.Profile{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, profileFetchTimeout)
	defer cancel()
	return s.profiles.Profile(ctx, playerprofile.Player{ID: player.ID.String(), ExternalIDs: externalIDs})
}

// saveProfile replaces the player's profile and all of its stats in one
// transaction, so a card never mixes stats from two fetches.
func (s *Service) saveProfile(ctx context.Context, playerID pgtype.UUID, profile playerprofile.Profile, fetchedAt time.Time) error {
	return s.write(ctx, func(ctx context.Context, q *store.Queries, _ pgtype.UUID) error {
		if err := q.UpsertPlayerProfile(ctx, profileParams(playerID, s.profiles.Name(), profile, fetchedAt)); err != nil {
			return err
		}
		if err := q.DeletePlayerProfileStats(ctx, playerID); err != nil {
			return err
		}
		return addProfileStats(ctx, q, playerID, profile.SeasonStats)
	})
}

func addProfileStats(ctx context.Context, q *store.Queries, playerID pgtype.UUID, stats []playerprofile.Stat) error {
	for ordinal, stat := range stats {
		err := q.AddPlayerProfileStat(ctx, store.AddPlayerProfileStatParams{
			PlayerID: playerID, Ordinal: int32(ordinal), Label: stat.Label, Value: numeric(stat.Value), Rank: optionalInt4(stat.Rank),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// profileParams maps a source profile to its row, storing unknown fields as NULL.
func profileParams(playerID pgtype.UUID, source string, profile playerprofile.Profile, fetchedAt time.Time) store.UpsertPlayerProfileParams {
	params := store.UpsertPlayerProfileParams{
		PlayerID:        playerID,
		Source:          source,
		Position:        optionalText(string(profile.Position)),
		HeadshotUrl:     optionalText(profile.HeadshotURL),
		Jersey:          optionalText(profile.Jersey),
		BirthDate:       optionalDate(profile.BirthDate),
		HeightInches:    optionalInt4(profile.HeightInches),
		WeightLbs:       optionalInt4(profile.WeightLbs),
		College:         optionalText(profile.College),
		PositionRank:    optionalInt4(profile.PositionRank),
		PercentRostered: optionalNumeric(profile.PercentRostered),
		PercentStarted:  optionalNumeric(profile.PercentStarted),
		SeasonOutlook:   optionalText(profile.SeasonOutlook),
		FetchedAt:       pgtype.Timestamptz{Time: fetchedAt, Valid: true},
	}
	if draft := profile.Draft; draft != nil {
		params.DraftYear, params.DraftRound, params.DraftPick = int4(draft.Year), int4(draft.Round), int4(draft.Pick)
	}
	if outlook := profile.WeeklyOutlook; outlook != nil {
		params.WeeklyOutlook, params.WeeklyOutlookWeek = optionalText(outlook.Text), int4(outlook.Week)
	}
	return params
}

// readPlayerCard builds the card from stored data: the player, their saved
// profile, their team's next game, and their latest news.
func (s *Service) readPlayerCard(ctx context.Context, player store.PlayerCardPlayerRow, now time.Time) (*v1.PlayerCard, error) {
	var card *v1.PlayerCard
	err := s.read(ctx, func(ctx context.Context, q *store.Queries, _ pgtype.UUID) error {
		saved, err := loadSavedProfile(ctx, q, player.ID)
		if err != nil {
			return err
		}
		nextGame, err := loadNextGame(ctx, q, player.ProTeam, now)
		if err != nil {
			return err
		}
		latestNews, err := loadLatestNews(ctx, q, player.ID, now)
		if err != nil {
			return err
		}
		card = playerCardMessage(player, saved, nextGame, latestNews, now)
		return nil
	})
	return card, err
}

func loadSavedProfile(ctx context.Context, q *store.Queries, playerID pgtype.UUID) (*savedProfile, error) {
	profile, err := q.GetPlayerProfile(ctx, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stats, err := q.ListPlayerProfileStats(ctx, playerID)
	if err != nil {
		return nil, err
	}
	return &savedProfile{profile: profile, stats: stats}, nil
}

func loadNextGame(ctx context.Context, q *store.Queries, proTeam string, now time.Time) (*store.NextNFLGameRow, error) {
	if proTeam == "" {
		return nil, nil
	}
	game, err := q.NextNFLGame(ctx, store.NextNFLGameParams{Team: proTeam, Now: pgtype.Timestamptz{Time: now, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &game, nil
}

// loadLatestNews returns the player's newest saved news from the past week,
// matching the window the Overview's player updates use.
func loadLatestNews(ctx context.Context, q *store.Queries, playerID pgtype.UUID, now time.Time) (*store.PlayerNews, error) {
	since := pgtype.Timestamptz{Time: now.Add(-playerCardNewsAge), Valid: true}
	news, err := q.LatestPlayerNews(ctx, store.LatestPlayerNewsParams{PlayerID: playerID, Since: since})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &news, nil
}

func optionalText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func int4(value int) pgtype.Int4 { return pgtype.Int4{Int32: int32(value), Valid: true} }

func optionalInt4(value *int) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return int4(*value)
}

func optionalDate(value *time.Time) pgtype.Date {
	if value == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *value, Valid: true}
}

func numeric(value float64) pgtype.Numeric {
	var result pgtype.Numeric
	_ = result.Scan(strconv.FormatFloat(value, 'f', -1, 64))
	return result
}

func optionalNumeric(value *float64) pgtype.Numeric {
	if value == nil {
		return pgtype.Numeric{}
	}
	return numeric(*value)
}
