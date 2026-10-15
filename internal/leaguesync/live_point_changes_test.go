package leaguesync

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestLineupValuesChanged(t *testing.T) {
	for _, test := range []struct {
		name   string
		before store.ListLiveLineupObservationsRow
		after  store.UpdateLiveLineupEntryRow
		want   bool
	}{
		{
			name:   "points differ at four decimals",
			before: store.ListLiveLineupObservationsRow{Points: numericValue(t, "10.0000")},
			after:  store.UpdateLiveLineupEntryRow{Points: numericValue(t, "10.0001")},
			want:   true,
		},
		{
			name:   "same points and counts",
			before: store.ListLiveLineupObservationsRow{Points: numericValue(t, "10.0000"), Touchdowns: int4Value(1)},
			after:  store.UpdateLiveLineupEntryRow{Points: numericValue(t, "10.0000"), Touchdowns: int4Value(1)},
		},
		{
			name:   "known touchdown count changes",
			before: store.ListLiveLineupObservationsRow{Points: numericValue(t, "10.0000"), Touchdowns: int4Value(1)},
			after:  store.UpdateLiveLineupEntryRow{Points: numericValue(t, "10.0000"), Touchdowns: int4Value(2)},
			want:   true,
		},
		{
			name:   "known field goal count changes",
			before: store.ListLiveLineupObservationsRow{FieldGoals: int4Value(1)},
			after:  store.UpdateLiveLineupEntryRow{FieldGoals: int4Value(2)},
			want:   true,
		},
		{
			name:  "unknown count becomes known",
			after: store.UpdateLiveLineupEntryRow{Touchdowns: int4Value(1)},
		},
		{
			name:   "null points and zero points are equivalent",
			before: store.ListLiveLineupObservationsRow{},
			after:  store.UpdateLiveLineupEntryRow{Points: numericValue(t, "0.0000")},
		},
		{
			name:  "null points become nonzero",
			after: store.UpdateLiveLineupEntryRow{Points: numericValue(t, "0.0001")},
			want:  true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := lineupValuesChanged(test.before, test.after)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("lineupValuesChanged() = %t, want %t", got, test.want)
			}
		})
	}
}

func numericValue(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil {
		t.Fatal(err)
	}
	return numeric
}

func int4Value(value int32) pgtype.Int4 {
	return pgtype.Int4{Int32: value, Valid: true}
}
