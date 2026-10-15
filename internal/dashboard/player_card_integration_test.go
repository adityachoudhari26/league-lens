//go:build integration

package dashboard_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1/leaguelensv1connect"
	"github.com/adityachoudhari26/league-lens/internal/httpserver"
	"github.com/adityachoudhari26/league-lens/internal/playerprofile"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// fakeProfiles returns a set profile or error and records who it was asked for.
type fakeProfiles struct {
	profile playerprofile.Profile
	err     error
	asked   []playerprofile.Player
}

func (*fakeProfiles) Name() string { return "fake" }

func (f *fakeProfiles) Profile(_ context.Context, player playerprofile.Player) (playerprofile.Profile, error) {
	f.asked = append(f.asked, player)
	return f.profile, f.err
}

func playerCardClient(t *testing.T, db *database.DB, owner string, profiles playerprofile.Source) leaguelensv1connect.DashboardServiceClient {
	t.Helper()
	server := httptest.NewServer(httpserver.NewHandler(db.Ping, dashboard.New(db, owner, profiles), nil))
	t.Cleanup(server.Close)
	return leaguelensv1connect.NewDashboardServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithProtoJSON())))
}

func getPlayerCard(t *testing.T, client leaguelensv1connect.DashboardServiceClient, playerID string) *v1.PlayerCard {
	t.Helper()
	response, err := client.GetPlayerCard(context.Background(), &v1.GetPlayerCardRequest{PlayerId: playerID})
	if err != nil {
		t.Fatal(err)
	}
	return response.Card
}

func TestPlayerCardFetchesLiveAndFallsBackToSaved(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	importLeague(t, db, "card-owner@test", fantasy.ProviderESPN)
	player, err := db.Queries().FindPlayerByExternalID(ctx, store.FindPlayerByExternalIDParams{Provider: string(fantasy.ProviderESPN), ExternalID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	rank := 3
	profiles := &fakeProfiles{profile: playerprofile.Profile{
		Position:      fantasy.PositionRB,
		HeadshotURL:   "https://a.espncdn.com/headshot.png",
		Draft:         &playerprofile.Draft{Year: 2023, Round: 1, Pick: 8},
		SeasonOutlook: "Lead back.",
		SeasonStats:   []playerprofile.Stat{{Label: "CAR", Value: 120, Rank: &rank}, {Label: "YDS/CAR", Value: 5.686}},
	}}
	client := playerCardClient(t, db, "card-owner@test", profiles)

	fetched := getPlayerCard(t, client, player.ID.String())
	if fetched.PlayerName != "Fixture Player" || fetched.Position != v1.PlayerPosition_PLAYER_POSITION_RB || fetched.Draft.GetPick() != 8 {
		t.Fatalf("fetched card: %v", fetched)
	}
	if len(fetched.SeasonStats) != 2 || fetched.SeasonStats[0].GetRank() != 3 || fetched.SeasonStats[1].Value != 5.686 {
		t.Fatalf("stats not saved in source order: %v", fetched.SeasonStats)
	}
	if fetched.Blurb.GetKind() != v1.PlayerBlurbKind_PLAYER_BLURB_KIND_SEASON_OUTLOOK || fetched.ProfileFetchedAt == nil {
		t.Fatalf("blurb or fetched_at missing: %v", fetched)
	}
	if profiles.asked[0].ExternalIDs[string(fantasy.ProviderESPN)] != "p" {
		t.Fatalf("source not given external IDs: %v", profiles.asked)
	}

	profiles.profile = playerprofile.Profile{SeasonStats: []playerprofile.Stat{{Label: "CAR", Value: 130}}}
	refetched := getPlayerCard(t, client, player.ID.String())
	if len(refetched.SeasonStats) != 1 || refetched.SeasonStats[0].Value != 130 || refetched.Position != v1.PlayerPosition_PLAYER_POSITION_UNSPECIFIED {
		t.Fatalf("every open should replace the saved profile: %v", refetched)
	}

	profiles.err = errors.New("source unavailable")
	fallback := getPlayerCard(t, client, player.ID.String())
	if len(fallback.SeasonStats) != 1 || fallback.SeasonStats[0].Value != 130 || !fallback.ProfileFetchedAt.AsTime().Equal(refetched.ProfileFetchedAt.AsTime()) {
		t.Fatalf("failed fetch should serve the saved profile: %v", fallback)
	}
	if len(profiles.asked) != 3 {
		t.Fatalf("each open should fetch live, got %d fetches", len(profiles.asked))
	}
}

func TestPlayerCardWithoutProfile(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	importLeague(t, db, "card-owner@test", fantasy.ProviderSleeper)
	defense, err := db.Queries().CreateEncounteredPlayer(ctx, store.CreateEncounteredPlayerParams{DisplayName: "Falcons D/ST", EntityType: string(fantasy.PlayerTeamDefense)})
	if err != nil {
		t.Fatal(err)
	}
	client := playerCardClient(t, db, "card-owner@test", &fakeProfiles{err: playerprofile.ErrUnknownPlayer})

	card := getPlayerCard(t, client, defense.ID.String())
	if card.PlayerName != "Falcons D/ST" || card.PlayerKind != v1.PlayerKind_PLAYER_KIND_TEAM_DEFENSE || card.ProfileFetchedAt != nil {
		t.Fatalf("unknown player should get a card without a profile: %v", card)
	}

	_, err = client.GetPlayerCard(ctx, &v1.GetPlayerCardRequest{PlayerId: "00000000-0000-0000-0000-000000000001"})
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown player_id: got %v", err)
	}
	_, err = client.GetPlayerCard(ctx, &v1.GetPlayerCardRequest{PlayerId: "invalid"})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("invalid player_id: got %v", err)
	}
}
