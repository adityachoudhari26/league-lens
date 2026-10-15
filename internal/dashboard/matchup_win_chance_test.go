package dashboard

import (
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestWinChanceSampleMessage(t *testing.T) {
	observedAt := time.Date(2026, time.October, 18, 13, 0, 0, 0, time.UTC)
	row := store.MatchupDetailWinChanceSamplesRow{
		ObservedAt:     pgtype.Timestamptz{Time: observedAt, Valid: true},
		TeamAWinChance: numeric(0.75),
		ScoreA:         numeric(12.5),
		ScoreB:         numeric(8.5),
		ProjectedA:     numeric(20.25),
		ProjectedB:     numeric(17.75),
	}
	tests := []struct {
		name           string
		ownerIsTeamB   bool
		wantWinChance  float64
		wantTeamScore  float64
		wantOtherScore float64
		wantTeamProj   float64
		wantOtherProj  float64
	}{
		{
			name: "owner is team A", wantWinChance: 0.75,
			wantTeamScore: 12.5, wantOtherScore: 8.5, wantTeamProj: 20.25, wantOtherProj: 17.75,
		},
		{
			name: "owner is team B", ownerIsTeamB: true, wantWinChance: 0.25,
			wantTeamScore: 8.5, wantOtherScore: 12.5, wantTeamProj: 17.75, wantOtherProj: 20.25,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := winChanceSampleMessage(row, test.ownerIsTeamB)
			if err != nil {
				t.Fatal(err)
			}
			if !got.ObservedAt.AsTime().Equal(observedAt) {
				t.Fatalf("observed_at = %v, want %v", got.ObservedAt, observedAt)
			}
			assertClose := func(field string, actual, want float64) {
				t.Helper()
				if math.Abs(actual-want) > 1e-9 {
					t.Errorf("%s = %v, want %v", field, actual, want)
				}
			}
			assertClose("team win chance", got.TeamWinChance, test.wantWinChance)
			assertClose("team score", got.TeamScore, test.wantTeamScore)
			assertClose("opponent score", got.OpponentScore, test.wantOtherScore)
			assertClose("team projected", got.TeamProjected, test.wantTeamProj)
			assertClose("opponent projected", got.OpponentProjected, test.wantOtherProj)
		})
	}
}

func TestKeyMomentMessage(t *testing.T) {
	int4 := func(value int32) pgtype.Int4 {
		return pgtype.Int4{Int32: value, Valid: true}
	}
	tests := []struct {
		name             string
		pointsBefore     float64
		pointsAfter      float64
		touchdownsBefore pgtype.Int4
		touchdownsAfter  pgtype.Int4
		fieldGoalsBefore pgtype.Int4
		fieldGoalsAfter  pgtype.Int4
		wantClassified   bool
		wantKind         v1.KeyMomentKind
		wantPoints       float64
	}{
		{
			name: "touchdown", pointsAfter: 6.8,
			touchdownsBefore: int4(0), touchdownsAfter: int4(1),
			wantClassified: true, wantKind: v1.KeyMomentKind_KEY_MOMENT_KIND_TOUCHDOWN, wantPoints: 6.8,
		},
		{
			name: "small change is skipped", pointsAfter: 0.5,
			touchdownsBefore: int4(0), touchdownsAfter: int4(0),
			fieldGoalsBefore: int4(0), fieldGoalsAfter: int4(0),
		},
		{
			name: "negative play", pointsBefore: 1.5,
			touchdownsBefore: int4(0), touchdownsAfter: int4(0),
			fieldGoalsBefore: int4(0), fieldGoalsAfter: int4(0),
			wantClassified: true, wantKind: v1.KeyMomentKind_KEY_MOMENT_KIND_PLAY, wantPoints: -1.5,
		},
		{
			name: "field goal", pointsAfter: 3,
			touchdownsBefore: int4(0), touchdownsAfter: int4(0),
			fieldGoalsBefore: int4(0), fieldGoalsAfter: int4(1),
			wantClassified: true, wantKind: v1.KeyMomentKind_KEY_MOMENT_KIND_FIELD_GOAL, wantPoints: 3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := store.MatchupDetailPointChangesRow{
				PointsBefore:     numeric(test.pointsBefore),
				PointsAfter:      numeric(test.pointsAfter),
				TouchdownsBefore: test.touchdownsBefore,
				TouchdownsAfter:  test.touchdownsAfter,
				FieldGoalsBefore: test.fieldGoalsBefore,
				FieldGoalsAfter:  test.fieldGoalsAfter,
			}
			got, classified, err := keyMomentMessage(row)
			if err != nil {
				t.Fatal(err)
			}
			if classified != test.wantClassified {
				t.Fatalf("classified = %t, want %t", classified, test.wantClassified)
			}
			if !classified {
				if got != nil {
					t.Fatalf("unclassified moment = %+v, want nil", got)
				}
				return
			}
			if got.Kind != test.wantKind || math.Abs(got.Points-test.wantPoints) > 1e-9 {
				t.Fatalf("moment = %+v, want kind %v and points %v", got, test.wantKind, test.wantPoints)
			}
		})
	}
}
