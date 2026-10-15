package dashboard

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/playerprofile"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestPlayerBlurbOrder(t *testing.T) {
	outlookWeek6 := &savedProfile{profile: store.PlayerProfile{
		WeeklyOutlook:     pgtype.Text{String: "Week 6 outlook", Valid: true},
		WeeklyOutlookWeek: pgtype.Int4{Int32: 6, Valid: true},
		SeasonOutlook:     pgtype.Text{String: "Season outlook", Valid: true},
	}}
	week6Game := &store.NextNFLGameRow{Week: 6}
	week7Game := &store.NextNFLGameRow{Week: 7}
	news := &store.PlayerNews{Headline: "Headline", Summary: "News summary"}
	tests := []struct {
		name     string
		saved    *savedProfile
		game     *store.NextNFLGameRow
		news     *store.PlayerNews
		wantKind v1.PlayerBlurbKind
		wantText string
	}{
		{"this week's outlook beats news", outlookWeek6, week6Game, news, v1.PlayerBlurbKind_PLAYER_BLURB_KIND_WEEKLY_OUTLOOK, "Week 6 outlook"},
		{"last week's outlook falls back to news", outlookWeek6, week7Game, news, v1.PlayerBlurbKind_PLAYER_BLURB_KIND_NEWS, "News summary"},
		{"no next game falls back to news", outlookWeek6, nil, news, v1.PlayerBlurbKind_PLAYER_BLURB_KIND_NEWS, "News summary"},
		{"no news falls back to season outlook", outlookWeek6, week7Game, nil, v1.PlayerBlurbKind_PLAYER_BLURB_KIND_SEASON_OUTLOOK, "Season outlook"},
		{"news headline without summary", nil, nil, &store.PlayerNews{Headline: "Headline"}, v1.PlayerBlurbKind_PLAYER_BLURB_KIND_NEWS, "Headline"},
	}
	for _, test := range tests {
		blurb := playerBlurb(test.saved, test.game, test.news)
		if blurb.GetKind() != test.wantKind || blurb.GetText() != test.wantText {
			t.Errorf("%s: got %v %q", test.name, blurb.GetKind(), blurb.GetText())
		}
	}
	if blurb := playerBlurb(nil, week6Game, nil); blurb != nil {
		t.Errorf("no profile or news should have no blurb, got %v", blurb)
	}
}

func TestAgeOn(t *testing.T) {
	birthDate := pgtype.Date{Time: time.Date(2002, time.January, 30, 0, 0, 0, 0, time.UTC), Valid: true}
	if age := ageOn(birthDate, time.Date(2026, time.January, 29, 12, 0, 0, 0, time.UTC)); *age != 23 {
		t.Errorf("day before birthday: got %d", *age)
	}
	if age := ageOn(birthDate, time.Date(2026, time.January, 30, 0, 0, 0, 0, time.UTC)); *age != 24 {
		t.Errorf("on birthday: got %d", *age)
	}
	if age := ageOn(pgtype.Date{}, time.Now()); age != nil {
		t.Errorf("unknown birth date: got %d", *age)
	}
}

func TestProfileParamsStoreUnknownFieldsAsNull(t *testing.T) {
	params := profileParams(pgtype.UUID{}, "espn", playerprofile.Profile{}, time.Now())
	if params.Position.Valid || params.HeadshotUrl.Valid || params.College.Valid || params.DraftYear.Valid ||
		params.WeeklyOutlook.Valid || params.WeeklyOutlookWeek.Valid || params.PercentRostered.Valid || params.BirthDate.Valid {
		t.Fatalf("unknown fields must be NULL: %+v", params)
	}

	rostered := 99.92
	params = profileParams(pgtype.UUID{}, "espn", playerprofile.Profile{
		Position:        fantasy.PositionRB,
		Draft:           &playerprofile.Draft{Year: 2023, Round: 1, Pick: 8},
		WeeklyOutlook:   &playerprofile.Outlook{Week: 6, Text: "Outlook"},
		PercentRostered: &rostered,
	}, time.Now())
	if params.Position.String != "RB" || params.DraftPick.Int32 != 8 || params.WeeklyOutlookWeek.Int32 != 6 {
		t.Fatalf("known fields not stored: %+v", params)
	}
	if value, err := optionalScore(params.PercentRostered); err != nil || value == nil || *value != rostered {
		t.Fatalf("percent rostered: got %v", value)
	}
}

func TestPlayerCardWithoutProfileHasStoredDataOnly(t *testing.T) {
	player := store.PlayerCardPlayerRow{DisplayName: "Falcons D/ST", EntityType: string(fantasy.PlayerTeamDefense), ProTeam: "ATL"}
	card, err := playerCardMessage(player, nil, &store.NextNFLGameRow{Week: 7, Opponent: "SF"}, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if card.PlayerKind != v1.PlayerKind_PLAYER_KIND_TEAM_DEFENSE || card.ProTeam != "ATL" || card.NextGame.GetOpponent() != "SF" {
		t.Fatalf("stored data missing: %v", card)
	}
	if card.ProfileFetchedAt != nil || card.Position != v1.PlayerPosition_PLAYER_POSITION_UNSPECIFIED || card.Blurb != nil {
		t.Fatalf("profile fields should be empty: %v", card)
	}
	if card.InjuryStatus != v1.InjuryStatus_INJURY_STATUS_UNKNOWN {
		t.Fatalf("missing injury status should be unknown: %v", card.InjuryStatus)
	}
}
