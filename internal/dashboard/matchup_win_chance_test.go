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

func TestMergeSplitScoringChanges(t *testing.T) {
	type pointChange = store.MatchupDetailPointChangesRow
	type momentExpectation struct {
		row    int
		kind   v1.KeyMomentKind
		points float64
	}
	makeUUID := func(lastByte byte) pgtype.UUID {
		var id pgtype.UUID
		id.Bytes[15] = lastByte
		id.Valid = true
		return id
	}
	teamID := makeUUID(1)
	makeChange := func(
		observedAt time.Time,
		playerID byte,
		pointsBefore, pointsAfter float64,
		touchdownsBefore, touchdownsAfter int32,
		fieldGoalsBefore, fieldGoalsAfter int32,
	) pointChange {
		return pointChange{
			ObservedAt:       pgtype.Timestamptz{Time: observedAt, Valid: true},
			TeamID:           teamID,
			PlayerID:         makeUUID(playerID),
			DisplayName:      "Player",
			PointsBefore:     numeric(pointsBefore),
			PointsAfter:      numeric(pointsAfter),
			TouchdownsBefore: pgtype.Int4{Int32: touchdownsBefore, Valid: true},
			TouchdownsAfter:  pgtype.Int4{Int32: touchdownsAfter, Valid: true},
			FieldGoalsBefore: pgtype.Int4{Int32: fieldGoalsBefore, Valid: true},
			FieldGoalsAfter:  pgtype.Int4{Int32: fieldGoalsAfter, Valid: true},
		}
	}
	base := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		rows       []pointChange
		wantRows   []pointChange
		wantMoment *momentExpectation
	}{
		{
			name: "touchdown count first",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base.Add(time.Minute), 2, 0, 9.9, 1, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base.Add(time.Minute), 2, 0, 9.9, 0, 1, 0, 0),
			},
			wantMoment: &momentExpectation{row: 0, kind: v1.KeyMomentKind_KEY_MOMENT_KIND_TOUCHDOWN, points: 9.9},
		},
		{
			name: "points first",
			rows: []pointChange{
				makeChange(base, 2, 0, 9.9, 0, 0, 0, 0),
				makeChange(base.Add(time.Minute), 2, 9.9, 9.9, 0, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base, 2, 0, 9.9, 0, 1, 0, 0),
			},
			wantMoment: &momentExpectation{row: 0, kind: v1.KeyMomentKind_KEY_MOMENT_KIND_TOUCHDOWN, points: 9.9},
		},
		{
			name: "field goal count first",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 0, 0, 1),
				makeChange(base.Add(time.Minute), 2, 0, 3, 0, 0, 1, 1),
			},
			wantRows: []pointChange{
				makeChange(base.Add(time.Minute), 2, 0, 3, 0, 0, 0, 1),
			},
			wantMoment: &momentExpectation{row: 0, kind: v1.KeyMomentKind_KEY_MOMENT_KIND_FIELD_GOAL, points: 3},
		},
		{
			name: "four minute gap stays separate",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base.Add(4*time.Minute), 2, 0, 9.9, 1, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base.Add(4*time.Minute), 2, 0, 9.9, 1, 1, 0, 0),
			},
		},
		{
			name: "different players stay separate",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base, 3, 0, 9.9, 1, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base, 3, 0, 9.9, 1, 1, 0, 0),
			},
		},
		{
			name: "real touchdown and unrelated play stay separate",
			rows: []pointChange{
				makeChange(base, 2, 0, 6.8, 0, 1, 0, 0),
				makeChange(base.Add(time.Minute), 2, 6.8, 8, 1, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base, 2, 0, 6.8, 0, 1, 0, 0),
				makeChange(base.Add(time.Minute), 2, 6.8, 8, 1, 1, 0, 0),
			},
		},
		{
			name: "unpaired count-only row stays unchanged",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
			},
		},
		{
			name: "merged pair does not chain",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base.Add(time.Minute), 2, 0, 9.9, 1, 1, 0, 0),
				makeChange(base.Add(2*time.Minute), 2, 9.9, 11.1, 1, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base.Add(time.Minute), 2, 0, 9.9, 0, 1, 0, 0),
				makeChange(base.Add(2*time.Minute), 2, 9.9, 11.1, 1, 1, 0, 0),
			},
		},
		{
			name: "merged row is resorted by its later time",
			rows: []pointChange{
				makeChange(base, 2, 0, 0, 0, 1, 0, 0),
				makeChange(base.Add(30*time.Second), 3, 0, 1.2, 0, 0, 0, 0),
				makeChange(base.Add(time.Minute), 2, 0, 9.9, 1, 1, 0, 0),
			},
			wantRows: []pointChange{
				makeChange(base.Add(30*time.Second), 3, 0, 1.2, 0, 0, 0, 0),
				makeChange(base.Add(time.Minute), 2, 0, 9.9, 0, 1, 0, 0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := mergeSplitScoringChanges(test.rows)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(test.wantRows) {
				t.Fatalf("got %d rows, want %d", len(got), len(test.wantRows))
			}
			for index, want := range test.wantRows {
				actual := got[index]
				if !actual.ObservedAt.Valid || !actual.ObservedAt.Time.Equal(want.ObservedAt.Time) {
					t.Errorf("row %d observed_at = %v, want %v", index, actual.ObservedAt, want.ObservedAt)
				}
				if actual.TeamID != want.TeamID || actual.PlayerID != want.PlayerID || actual.DisplayName != want.DisplayName {
					t.Errorf("row %d identity = (%v, %v, %q), want (%v, %v, %q)",
						index, actual.TeamID, actual.PlayerID, actual.DisplayName, want.TeamID, want.PlayerID, want.DisplayName)
				}
				assertNumeric := func(field string, actual, expected pgtype.Numeric) {
					t.Helper()
					actualValue, err := requiredNumericValue(actual)
					if err != nil {
						t.Errorf("row %d %s: %v", index, field, err)
						return
					}
					expectedValue, err := requiredNumericValue(expected)
					if err != nil {
						t.Errorf("expected row %d %s: %v", index, field, err)
						return
					}
					if math.Abs(actualValue-expectedValue) > 1e-9 {
						t.Errorf("row %d %s = %v, want %v", index, field, actualValue, expectedValue)
					}
				}
				assertNumeric("points_before", actual.PointsBefore, want.PointsBefore)
				assertNumeric("points_after", actual.PointsAfter, want.PointsAfter)
				if actual.TouchdownsBefore != want.TouchdownsBefore || actual.TouchdownsAfter != want.TouchdownsAfter {
					t.Errorf("row %d touchdowns = %v→%v, want %v→%v",
						index, actual.TouchdownsBefore, actual.TouchdownsAfter, want.TouchdownsBefore, want.TouchdownsAfter)
				}
				if actual.FieldGoalsBefore != want.FieldGoalsBefore || actual.FieldGoalsAfter != want.FieldGoalsAfter {
					t.Errorf("row %d field goals = %v→%v, want %v→%v",
						index, actual.FieldGoalsBefore, actual.FieldGoalsAfter, want.FieldGoalsBefore, want.FieldGoalsAfter)
				}
			}
			if test.wantMoment != nil {
				moment, classified, err := keyMomentMessage(got[test.wantMoment.row])
				if err != nil {
					t.Fatal(err)
				}
				if !classified {
					t.Fatal("expected merged row to be classified")
				}
				if moment.Kind != test.wantMoment.kind || math.Abs(moment.Points-test.wantMoment.points) > 1e-9 {
					t.Fatalf("moment = %+v, want kind %v and points %v", moment, test.wantMoment.kind, test.wantMoment.points)
				}
			}
		})
	}
}
