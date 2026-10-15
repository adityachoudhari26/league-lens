package replay

import (
	"math"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestBuildReplaysMatchupAcrossGames(t *testing.T) {
	start := time.Date(2026, time.October, 9, 0, 18, 3, 0, time.UTC)
	gameOneEnd := start.Add(time.Hour)
	gameTwoStart := start.Add(15 * time.Minute)
	gameTwoEnd := start.Add(90 * time.Minute)
	playsOne := []fantasy.NFLPlay{
		{
			At: start, Period: 1, Clock: 13*time.Minute + 53*time.Second,
			Kind: fantasy.NFLPlayRush, Yards: 5,
			Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
		},
		{
			At: start.Add(30 * time.Second), Period: 1, Clock: 13*time.Minute + 23*time.Second,
			Kind: fantasy.NFLPlayRushingTouchdown, Yards: 1, ExtraPointGood: true,
			Participants: []fantasy.NFLPlayParticipant{
				{AthleteID: "rb", Role: "rusher"},
				{AthleteID: "rb", Role: "scorer"},
				{AthleteID: "k", Role: "kicker"},
				{AthleteID: "k", Role: "patScorer"},
			},
		},
		{
			At: start.Add(5 * time.Minute), Period: 1, Clock: 8 * time.Minute,
			Kind: fantasy.NFLPlayPassingTouchdown, Yards: 20,
			Participants: []fantasy.NFLPlayParticipant{
				{AthleteID: "qb1", Role: "passer"},
				{AthleteID: "wr", Role: "receiver"},
				{AthleteID: "wr", Role: "scorer"},
			},
		},
		{
			At: start.Add(10 * time.Minute), Period: 1, Clock: 3 * time.Minute,
			Kind:         fantasy.NFLPlayFieldGoalGood,
			Participants: []fantasy.NFLPlayParticipant{{AthleteID: "k", Role: "kicker"}},
		},
		{At: gameOneEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
	}
	playsTwo := []fantasy.NFLPlay{
		{
			At: gameTwoStart, Period: 1, Clock: 14 * time.Minute,
			Kind:         fantasy.NFLPlayInterception,
			Participants: []fantasy.NFLPlayParticipant{{AthleteID: "qb2", Role: "passer"}},
		},
		{At: gameTwoEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
	}
	projected := func(value float64) *float64 { return &value }

	timeline := Build(Input{
		Starters: []Starter{
			{Key: "rb", TeamB: true, AthleteID: "rb", ProTeam: "KC", FinalPoints: 8, Projected: projected(20)},
			{Key: "kicker", TeamB: true, AthleteID: "k", ProTeam: "KC", FinalPoints: 5, Projected: projected(20)},
			{Key: "qb1", AthleteID: "qb1", ProTeam: "SF", FinalPoints: 4.8, Projected: projected(25)},
			{Key: "wr", AthleteID: "wr", ProTeam: "SF", FinalPoints: 8.5, Projected: projected(25)},
			{Key: "qb2", AthleteID: "qb2", ProTeam: "MIA", FinalPoints: -2, Projected: projected(0)},
			{Key: "dst", TeamB: true, ProTeam: "MIA", FinalPoints: 7, Projected: projected(10)},
		},
		Games: []Game{
			{HomeTeam: "KC", AwayTeam: "SF", Plays: playsOne},
			{HomeTeam: "MIA", AwayTeam: "JAX", Plays: playsTwo},
		},
		Rules: []fantasy.ScoringRule{
			{Stat: fantasy.StatRushingYards, Points: 1, PerUnits: 10},
			{Stat: fantasy.StatRushingTouchdowns, Points: 6, PerUnits: 1},
			{Stat: fantasy.StatPassingYards, Points: 1, PerUnits: 25},
			{Stat: fantasy.StatPassingTouchdowns, Points: 4, PerUnits: 1},
			{Stat: fantasy.StatReceivingYards, Points: 1, PerUnits: 10},
			{Stat: fantasy.StatReceptions, Points: 0.5, PerUnits: 0},
			{Stat: fantasy.StatReceivingTouchdowns, Points: 6, PerUnits: 1},
			{Stat: fantasy.StatInterceptions, Points: -2, PerUnits: 1},
		},
	})

	firstBucket := start.Truncate(time.Minute)
	rbChange := changeAt(t, timeline, "rb", firstBucket)
	if rbChange.PointsAfter != 6.6 || rbChange.TouchdownsBefore != 0 || rbChange.TouchdownsAfter != 1 {
		t.Errorf("rushing touchdown change = %+v", rbChange)
	}
	if got := countChangesAt(timeline, "rb", firstBucket); got != 1 {
		t.Errorf("rushing plays in one minute produced %d changes, want 1", got)
	}
	kickerPAT := changeAt(t, timeline, "kicker", firstBucket)
	if kickerPAT.PointsAfter != 1 {
		t.Errorf("successful extra point change = %+v, want +1", kickerPAT)
	}

	passingTDAt := start.Add(5 * time.Minute).Truncate(time.Minute)
	qbChange := changeAt(t, timeline, "qb1", passingTDAt)
	wrChange := changeAt(t, timeline, "wr", passingTDAt)
	if qbChange.PointsAfter != 4.8 || qbChange.TouchdownsAfter != 1 {
		t.Errorf("passing touchdown QB change = %+v", qbChange)
	}
	if wrChange.PointsAfter != 8.5 || wrChange.TouchdownsAfter != 1 {
		t.Errorf("passing touchdown receiver change = %+v", wrChange)
	}

	fieldGoalAt := start.Add(10 * time.Minute).Truncate(time.Minute)
	fieldGoal := changeAt(t, timeline, "kicker", fieldGoalAt)
	if fieldGoal.PointsAfter-fieldGoal.PointsBefore != 3 ||
		fieldGoal.FieldGoalsBefore != 0 || fieldGoal.FieldGoalsAfter != 1 {
		t.Errorf("field goal change = %+v", fieldGoal)
	}
	interception := changeAt(t, timeline, "qb2", gameTwoStart.Truncate(time.Minute))
	if interception.PointsAfter != -2 {
		t.Errorf("interception change = %+v, want -2", interception)
	}

	residual := changeAt(t, timeline, "rb", gameOneEnd.Truncate(time.Minute))
	if residual.PointsBefore != 6.6 || residual.PointsAfter != 8 {
		t.Errorf("game-end residual = %+v, want 6.6 to 8", residual)
	}
	dst := changeAt(t, timeline, "dst", gameTwoEnd.Truncate(time.Minute))
	if dst.PointsBefore != 0 || dst.PointsAfter != 7 {
		t.Errorf("D/ST anchor = %+v, want 0 to 7 at latest game end", dst)
	}

	if len(timeline.Samples) < 2 {
		t.Fatalf("samples = %+v, want pregame and final samples", timeline.Samples)
	}
	pregame := timeline.Samples[0]
	if pregame.ScoreA != 0 || pregame.ScoreB != 0 || pregame.Estimate.WinChanceA != 0.5 {
		t.Errorf("pregame sample = %+v, want 0-0 and 0.5", pregame)
	}
	final := timeline.Samples[len(timeline.Samples)-1]
	if final.ScoreA != 11.3 || final.ScoreB != 20 {
		t.Errorf("final scores = %.4f-%.4f, want 11.3-20", final.ScoreA, final.ScoreB)
	}
	if final.Estimate.WinChanceA != 0 {
		t.Errorf("final win chance = %.4f, want 0", final.Estimate.WinChanceA)
	}
	if math.Abs(final.ScoreA-(4.8+8.5-2)) > 0.0001 ||
		math.Abs(final.ScoreB-(8+5+7)) > 0.0001 {
		t.Errorf("final sample does not match provider totals: %+v", final)
	}
}

func TestBuildReplaysWindowFromStartingValues(t *testing.T) {
	from := time.Date(2026, time.October, 9, 13, 0, 15, 0, time.UTC)
	to := from.Add(5*time.Minute + 30*time.Second)
	projected := 10.0
	timeline := Build(Input{
		From: from,
		To:   to,
		Starters: []Starter{{
			Key: "rb", AthleteID: "rb", ProTeam: "KC",
			StartPoints: 4, StartTouchdowns: 1, StartFieldGoals: 1,
			FinalPoints: 6, Projected: &projected,
		}},
		Games: []Game{{
			HomeTeam: "KC",
			Plays: []fantasy.NFLPlay{
				{
					At: from.Add(-time.Minute), Kind: fantasy.NFLPlayRushingTouchdown, Yards: 10,
					Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
				},
				{
					At: from.Add(30 * time.Second), Period: 2, Clock: 10 * time.Minute,
					Kind: fantasy.NFLPlayRush, Yards: 5,
					Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
				},
				{
					At: from.Add(90 * time.Second), Period: 2, Clock: 9 * time.Minute,
					Kind: fantasy.NFLPlayRush, Yards: 5,
					Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
				},
				{
					At: to.Add(time.Minute), Period: 2, Clock: 3 * time.Minute,
					Kind: fantasy.NFLPlayRushingTouchdown, Yards: 10,
					Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
				},
				{At: to.Add(2 * time.Minute), Kind: fantasy.NFLPlayEndOfGame},
			},
		}},
		Rules: []fantasy.ScoringRule{
			{Stat: fantasy.StatRushingYards, Points: 1, PerUnits: 10},
			{Stat: fantasy.StatRushingTouchdowns, Points: 6, PerUnits: 1},
		},
	})

	if len(timeline.Changes) != 2 {
		t.Fatalf("window changes = %+v, want one play bucket and one residual", timeline.Changes)
	}
	playChange := timeline.Changes[0]
	if !playChange.At.After(from) || playChange.PointsBefore != 4 || playChange.PointsAfter != 5 ||
		playChange.TouchdownsBefore != 1 || playChange.TouchdownsAfter != 1 ||
		playChange.FieldGoalsBefore != 1 || playChange.FieldGoalsAfter != 1 {
		t.Errorf("window play change = %+v, start values should carry through", playChange)
	}
	residual := timeline.Changes[1]
	if !residual.At.Equal(minuteBucket(to)) || residual.PointsBefore != 5 || residual.PointsAfter != 6 {
		t.Errorf("window residual = %+v, want 5 to 6 at To", residual)
	}
	for _, change := range timeline.Changes {
		if change.TouchdownsAfter != 1 {
			t.Errorf("out-of-window touchdown was credited: %+v", change)
		}
	}
	if len(timeline.Samples) == 0 || !timeline.Samples[0].At.After(from) ||
		timeline.Samples[0].ScoreA != 5 {
		t.Errorf("first window sample = %+v, want changed score after From", timeline.Samples)
	}
	final := timeline.Samples[len(timeline.Samples)-1]
	if final.ScoreA != 6 || final.Estimate.WinChanceA <= 0.5 {
		t.Errorf("window final sample = %+v, want in-window score and live estimate", final)
	}
}

func TestBuildWindowAnchorsResidualAtGameEnd(t *testing.T) {
	from := time.Date(2026, time.October, 9, 13, 0, 0, 0, time.UTC)
	gameEnd := from.Add(5 * time.Minute)
	timeline := Build(Input{
		From: from,
		To:   gameEnd.Add(time.Minute),
		Starters: []Starter{{
			Key: "rb", AthleteID: "rb", ProTeam: "KC", FinalPoints: 3,
		}},
		Games: []Game{{
			HomeTeam: "KC",
			Plays: []fantasy.NFLPlay{
				{
					At: from.Add(time.Minute), Period: 1, Clock: 13 * time.Minute,
					Kind: fantasy.NFLPlayRush, Yards: 5,
					Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
				},
				{At: gameEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
			},
		}},
		Rules: []fantasy.ScoringRule{{Stat: fantasy.StatRushingYards, Points: 1, PerUnits: 10}},
	})
	residual := changeAt(t, timeline, "rb", gameEnd.Truncate(time.Minute))
	if residual.PointsBefore != 0.5 || residual.PointsAfter != 3 {
		t.Errorf("game-end residual = %+v, want 0.5 to 3", residual)
	}
}

func TestBuildWindowWithoutGamesAnchorsResidualAtTo(t *testing.T) {
	to := time.Date(2026, time.October, 9, 13, 5, 30, 0, time.UTC)
	timeline := Build(Input{
		From:     to.Add(-6 * time.Minute),
		To:       to,
		Starters: []Starter{{Key: "dst", StartPoints: 2, FinalPoints: 7}},
	})
	if len(timeline.Changes) != 1 || timeline.Changes[0].PointsBefore != 2 ||
		timeline.Changes[0].PointsAfter != 7 {
		t.Fatalf("window residual changes = %+v, want 2 to 7", timeline.Changes)
	}
}

func changeAt(t *testing.T, timeline Timeline, key string, at time.Time) Change {
	t.Helper()
	for _, change := range timeline.Changes {
		if change.Key == key && change.At.Equal(at) {
			return change
		}
	}
	t.Fatalf("no change for %q at %s in %+v", key, at, timeline.Changes)
	return Change{}
}

func countChangesAt(timeline Timeline, key string, at time.Time) int {
	count := 0
	for _, change := range timeline.Changes {
		if change.Key == key && change.At.Equal(at) {
			count++
		}
	}
	return count
}
