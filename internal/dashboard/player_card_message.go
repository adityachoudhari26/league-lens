package dashboard

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// playerCardMessage combines the stored player with their saved profile, if
// any. Without a profile, every profile field stays empty or absent.
func playerCardMessage(
	player store.PlayerCardPlayerRow,
	saved *savedProfile,
	nextGame *store.NextNFLGameRow,
	latestNews *store.PlayerNews,
	now time.Time,
) *v1.PlayerCard {
	card := &v1.PlayerCard{
		PlayerId:     player.ID.String(),
		PlayerName:   player.DisplayName,
		PlayerKind:   playerKindMessage(fantasy.PlayerKind(player.EntityType)),
		ProTeam:      player.ProTeam,
		InjuryStatus: normalizedInjuryStatus(player.InjuryStatus),
		NextGame:     nextGameMessage(nextGame),
		Blurb:        playerBlurb(saved, nextGame, latestNews),
	}
	if saved != nil {
		addProfileFields(card, saved, now)
	}
	return card
}

func addProfileFields(card *v1.PlayerCard, saved *savedProfile, now time.Time) {
	profile := saved.profile
	card.Position = positionMessage(fantasy.Position(profile.Position.String))
	card.HeadshotUrl = profile.HeadshotUrl.String
	card.Jersey = profile.Jersey.String
	card.Age = ageOn(profile.BirthDate, now)
	card.HeightInches = optionalInt32(profile.HeightInches)
	card.WeightLbs = optionalInt32(profile.WeightLbs)
	card.College = profile.College.String
	card.Draft = draftMessage(profile)
	card.PositionRank = optionalInt32(profile.PositionRank)
	card.PercentRostered = optionalFloat64(profile.PercentRostered)
	card.PercentStarted = optionalFloat64(profile.PercentStarted)
	card.SeasonStats = statMessages(saved.stats)
	card.ProfileFetchedAt = timestamp(profile.FetchedAt)
}

// playerBlurb picks this week's outlook, then news from the past week, then the
// season outlook. An outlook is this week's only when it is for the week of the
// player's next game; ESPN keeps showing last week's until a new one is written.
func playerBlurb(saved *savedProfile, nextGame *store.NextNFLGameRow, latestNews *store.PlayerNews) *v1.PlayerBlurb {
	if blurb := weeklyOutlookBlurb(saved, nextGame); blurb != nil {
		return blurb
	}
	if latestNews != nil {
		return newsBlurb(*latestNews)
	}
	return seasonOutlookBlurb(saved)
}

func weeklyOutlookBlurb(saved *savedProfile, nextGame *store.NextNFLGameRow) *v1.PlayerBlurb {
	if saved == nil || nextGame == nil || !saved.profile.WeeklyOutlook.Valid {
		return nil
	}
	week := saved.profile.WeeklyOutlookWeek.Int32
	if week != nextGame.Week {
		return nil
	}
	return &v1.PlayerBlurb{Kind: v1.PlayerBlurbKind_PLAYER_BLURB_KIND_WEEKLY_OUTLOOK, Text: saved.profile.WeeklyOutlook.String, Week: &week}
}

// newsBlurb uses the news summary, or the headline when a story has none.
func newsBlurb(news store.PlayerNews) *v1.PlayerBlurb {
	text := news.Summary
	if text == "" {
		text = news.Headline
	}
	return &v1.PlayerBlurb{Kind: v1.PlayerBlurbKind_PLAYER_BLURB_KIND_NEWS, Text: text, PublishedAt: timestamp(news.PublishedAt), Url: news.Url}
}

func seasonOutlookBlurb(saved *savedProfile) *v1.PlayerBlurb {
	if saved == nil || !saved.profile.SeasonOutlook.Valid {
		return nil
	}
	return &v1.PlayerBlurb{Kind: v1.PlayerBlurbKind_PLAYER_BLURB_KIND_SEASON_OUTLOOK, Text: saved.profile.SeasonOutlook.String}
}

func nextGameMessage(game *store.NextNFLGameRow) *v1.NextGame {
	if game == nil {
		return nil
	}
	return &v1.NextGame{Opponent: game.Opponent, KickoffAt: timestamp(game.KickoffAt)}
}

func draftMessage(profile store.PlayerProfile) *v1.PlayerDraft {
	if !profile.DraftYear.Valid {
		return nil
	}
	return &v1.PlayerDraft{Year: profile.DraftYear.Int32, Round: profile.DraftRound.Int32, Pick: profile.DraftPick.Int32}
}

func statMessages(stats []store.PlayerProfileStat) []*v1.PlayerStat {
	messages := make([]*v1.PlayerStat, 0, len(stats))
	for _, stat := range stats {
		value, _ := stat.Value.Float64Value()
		messages = append(messages, &v1.PlayerStat{Label: stat.Label, Value: value.Float64, Rank: optionalInt32(stat.Rank)})
	}
	return messages
}

// ageOn counts whole years since birth, so a player is a year older only once
// their birthday has passed.
func ageOn(birthDate pgtype.Date, now time.Time) *int32 {
	if !birthDate.Valid {
		return nil
	}
	born := birthDate.Time
	age := int32(now.Year() - born.Year())
	if now.Month() < born.Month() || (now.Month() == born.Month() && now.Day() < born.Day()) {
		age--
	}
	return &age
}

func playerKindMessage(kind fantasy.PlayerKind) v1.PlayerKind {
	kinds := map[fantasy.PlayerKind]v1.PlayerKind{
		fantasy.PlayerPerson:      v1.PlayerKind_PLAYER_KIND_PERSON,
		fantasy.PlayerTeamDefense: v1.PlayerKind_PLAYER_KIND_TEAM_DEFENSE,
	}
	return kinds[kind]
}

func positionMessage(position fantasy.Position) v1.PlayerPosition {
	positions := map[fantasy.Position]v1.PlayerPosition{
		fantasy.PositionQB:  v1.PlayerPosition_PLAYER_POSITION_QB,
		fantasy.PositionRB:  v1.PlayerPosition_PLAYER_POSITION_RB,
		fantasy.PositionWR:  v1.PlayerPosition_PLAYER_POSITION_WR,
		fantasy.PositionTE:  v1.PlayerPosition_PLAYER_POSITION_TE,
		fantasy.PositionK:   v1.PlayerPosition_PLAYER_POSITION_K,
		fantasy.PositionDEF: v1.PlayerPosition_PLAYER_POSITION_DEF,
	}
	return positions[position]
}

func optionalInt32(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	return &value.Int32
}

func optionalFloat64(value pgtype.Numeric) *float64 {
	result, err := value.Float64Value()
	if err != nil || !result.Valid {
		return nil
	}
	return &result.Float64
}
