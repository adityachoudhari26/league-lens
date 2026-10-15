package keymoments

import (
	"math"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, test := range []struct {
		name   string
		change PointChange
		want   Moment
		wantOK bool
	}{
		{
			name:   "touchdown",
			change: PointChange{PointsBefore: 10, PointsAfter: 16.8, TouchdownsBefore: intPointer(0), TouchdownsAfter: intPointer(1)},
			want:   Moment{Kind: KindTouchdown, Points: 6.8},
			wantOK: true,
		},
		{
			name:   "field goal",
			change: PointChange{PointsBefore: 10, PointsAfter: 13, FieldGoalsBefore: intPointer(0), FieldGoalsAfter: intPointer(1)},
			want:   Moment{Kind: KindFieldGoal, Points: 3},
			wantOK: true,
		},
		{
			name: "touchdown takes priority",
			change: PointChange{
				PointsBefore: 0, PointsAfter: 9.8,
				TouchdownsBefore: intPointer(0), TouchdownsAfter: intPointer(1),
				FieldGoalsBefore: intPointer(0), FieldGoalsAfter: intPointer(1),
			},
			want:   Moment{Kind: KindTouchdown, Points: 9.8},
			wantOK: true,
		},
		{name: "small change", change: PointChange{PointsBefore: 5, PointsAfter: 5.6}},
		{
			name:   "exact threshold",
			change: PointChange{PointsBefore: 5, PointsAfter: 6},
			want:   Moment{Kind: KindPlay, Points: 1},
			wantOK: true,
		},
		{
			name:   "threshold tolerance",
			change: PointChange{PointsAfter: 0.9999999995},
			want:   Moment{Kind: KindPlay, Points: 0.9999999995},
			wantOK: true,
		},
		{
			name:   "negative play",
			change: PointChange{PointsBefore: 5, PointsAfter: 3.5},
			want:   Moment{Kind: KindPlay, Points: -1.5},
			wantOK: true,
		},
		{
			name:   "unknown count increase falls through to play",
			change: PointChange{PointsBefore: 10, PointsAfter: 16, TouchdownsAfter: intPointer(1)},
			want:   Moment{Kind: KindPlay, Points: 6},
			wantOK: true,
		},
		{
			name:   "decreased count falls through to play",
			change: PointChange{PointsBefore: 10, PointsAfter: 4, TouchdownsBefore: intPointer(1), TouchdownsAfter: intPointer(0)},
			want:   Moment{Kind: KindPlay, Points: -6},
			wantOK: true,
		},
		{
			name:   "touchdown with zero point change",
			change: PointChange{PointsBefore: 10, PointsAfter: 10, TouchdownsBefore: intPointer(0), TouchdownsAfter: intPointer(1)},
			want:   Moment{Kind: KindTouchdown},
			wantOK: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Classify(test.change)
			if ok != test.wantOK {
				t.Fatalf("Classify() ok = %t, want %t", ok, test.wantOK)
			}
			if !ok {
				return
			}
			if got.Kind != test.want.Kind || math.Abs(got.Points-test.want.Points) > 1e-9 {
				t.Fatalf("Classify() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func intPointer(value int) *int {
	return &value
}
