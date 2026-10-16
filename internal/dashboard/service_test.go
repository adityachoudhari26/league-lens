package dashboard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type unavailableDB struct{}

func (unavailableDB) InReadTx(context.Context, func(*store.Queries) error) error {
	return errors.New("password=secret")
}

func (unavailableDB) InTx(context.Context, func(*store.Queries) error) error {
	return errors.New("password=secret")
}

func TestInvalidRequestsFailBeforeDatabaseRead(t *testing.T) {
	s := New(unavailableDB{}, "owner@localhost", nil, nil, nil)
	calls := []func() error{
		func() error {
			_, e := s.GetLeague(context.Background(), &v1.GetLeagueRequest{LeagueSeasonId: "invalid"})
			return e
		},
		func() error {
			_, e := s.ListLeagues(context.Background(), &v1.ListLeaguesRequest{PageSize: 101})
			return e
		},
		func() error {
			_, e := s.ListLeagues(context.Background(), &v1.ListLeaguesRequest{PageToken: "invalid"})
			return e
		},
		func() error {
			_, e := s.ListMatchups(context.Background(), &v1.ListMatchupsRequest{LeagueSeasonId: "00000000-0000-0000-0000-000000000001"})
			return e
		},
		func() error {
			week := int32(0)
			_, e := s.GetTeamRoster(context.Background(), &v1.GetTeamRosterRequest{TeamId: "00000000-0000-0000-0000-000000000001", ScoringPeriod: &week})
			return e
		},
		func() error {
			_, e := s.DismissLineupAlert(context.Background(), &v1.DismissLineupAlertRequest{})
			return e
		},
		func() error {
			_, e := s.RestoreLineupAlert(context.Background(), &v1.RestoreLineupAlertRequest{Fingerprint: strings.Repeat("x", 201)})
			return e
		},
	}
	for _, call := range calls {
		if err := call(); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("wrong validation error: %v", err)
		}
	}
}

func TestDatabaseErrorsAreSanitized(t *testing.T) {
	s := New(unavailableDB{}, "owner@localhost", nil, nil, nil)
	_, err := s.ListLeagues(context.Background(), &v1.ListLeaguesRequest{})
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
	_, err = s.DismissLineupAlert(context.Background(), &v1.DismissLineupAlertRequest{Fingerprint: "alert:week1"})
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe write error: %v", err)
	}
}
