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
		{
			At: gameTwoStart.Add(5 * time.Minute), Period: 1, Clock: 9 * time.Minute,
			Kind:         fantasy.NFLPlayFieldGoalGood,
			Participants: []fantasy.NFLPlayParticipant{{AthleteID: "fgk", Role: "kicker"}},
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
			{Key: "fgk", TeamB: true, AthleteID: "fgk", ProTeam: "MIA", FinalPoints: 5},
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

	if countChangesAt(timeline, "rb", gameOneEnd.Truncate(time.Minute)) != 0 {
		t.Errorf("game-end residual produced a change: %+v", timeline.Changes)
	}
	fgAt := gameTwoStart.Add(5 * time.Minute).Truncate(time.Minute)
	fgKicker := changeAt(t, timeline, "fgk", fgAt)
	if fgKicker.PointsBefore != 0 || fgKicker.PointsAfter != 3 ||
		fgKicker.FieldGoalsBefore != 0 || fgKicker.FieldGoalsAfter != 1 {
		t.Errorf("FG kicker change = %+v, want 0 to 3 for one FG", fgKicker)
	}
	if countChangesAt(timeline, "fgk", gameTwoEnd.Truncate(time.Minute)) != 0 {
		t.Errorf("FG residual produced a change: %+v", timeline.Changes)
	}
	if countChangesAt(timeline, "dst", gameTwoEnd.Truncate(time.Minute)) != 0 {
		t.Errorf("D/ST residual produced a change: %+v", timeline.Changes)
	}
	gameEndSample := sampleAt(t, timeline, gameTwoEnd.Truncate(time.Minute))
	if gameEndSample.ScoreB != 25 {
		t.Errorf("game-end sample score B = %.4f, want residual-adjusted 25", gameEndSample.ScoreB)
	}

	if len(timeline.Samples) < 2 {
		t.Fatalf("samples = %+v, want pregame and final samples", timeline.Samples)
	}
	pregame := timeline.Samples[0]
	if pregame.ScoreA != 0 || pregame.ScoreB != 0 || pregame.Estimate.WinChanceA != 0.5 {
		t.Errorf("pregame sample = %+v, want 0-0 and 0.5", pregame)
	}
	final := timeline.Samples[len(timeline.Samples)-1]
	if final.ScoreA != 11.3 || final.ScoreB != 25 {
		t.Errorf("final scores = %.4f-%.4f, want 11.3-25", final.ScoreA, final.ScoreB)
	}
	if final.Estimate.WinChanceA != 0 {
		t.Errorf("final win chance = %.4f, want 0", final.Estimate.WinChanceA)
	}
	if math.Abs(final.ScoreA-(4.8+8.5-2)) > 0.0001 ||
		math.Abs(final.ScoreB-(8+5+7+5)) > 0.0001 {
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

	if len(timeline.Changes) != 1 {
		t.Fatalf("window changes = %+v, want only the credited play bucket", timeline.Changes)
	}
	playChange := timeline.Changes[0]
	if !playChange.At.After(from) || playChange.PointsBefore != 4 || playChange.PointsAfter != 5 ||
		playChange.TouchdownsBefore != 1 || playChange.TouchdownsAfter != 1 ||
		playChange.FieldGoalsBefore != 1 || playChange.FieldGoalsAfter != 1 {
		t.Errorf("window play change = %+v, start values should carry through", playChange)
	}
	if countChangesAt(timeline, "rb", minuteBucket(to)) != 0 {
		t.Errorf("window residual produced a change: %+v", timeline.Changes)
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
	if !final.At.Equal(minuteBucket(to)) {
		t.Errorf("window adjustment sample time = %s, want bucket %s", final.At, minuteBucket(to))
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
	if len(timeline.Changes) != 1 {
		t.Fatalf("game-end residual changes = %+v, want only the credited rush", timeline.Changes)
	}
	gameEndSample := sampleAt(t, timeline, gameEnd.Truncate(time.Minute))
	if gameEndSample.ScoreA != 3 {
		t.Errorf("game-end adjustment sample = %+v, want score 3", gameEndSample)
	}
}

func TestBuildWindowKeepsProjectionForUnplayedGame(t *testing.T) {
	from := time.Date(2026, time.October, 9, 19, 0, 0, 0, time.UTC)
	gameEnd := from.Add(5 * time.Minute)
	projected := 20.0
	timeline := Build(Input{
		From: from,
		To:   from.Add(10 * time.Minute),
		Starters: []Starter{
			{Key: "kc", ProTeam: "KC", FinalPoints: 79},
			{Key: "sf", TeamB: true, ProTeam: "SF", StartPoints: 79, FinalPoints: 79, Projected: &projected},
		},
		Games: []Game{
			{HomeTeam: "KC", Plays: []fantasy.NFLPlay{
				{At: gameEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
			}},
			{HomeTeam: "SF"},
		},
	})
	if len(timeline.Samples) == 0 {
		t.Fatal("window has no samples")
	}
	for _, sample := range timeline.Samples {
		if sample.Estimate.ProjectedB != 99 {
			t.Errorf("sample at %s projected B = %.4f, want score 79 plus projection 20",
				sample.At, sample.Estimate.ProjectedB)
		}
		if sample.Estimate.WinChanceA >= 0.99 {
			t.Errorf("sample at %s win chance A = %.4f, want less than 0.99",
				sample.At, sample.Estimate.WinChanceA)
		}
	}
}

func TestBuildWindowWithoutGamesAnchorsResidualAtTo(t *testing.T) {
	to := time.Date(2026, time.October, 9, 13, 5, 30, 0, time.UTC)
	timeline := Build(Input{
		From:     to.Add(-6 * time.Minute),
		To:       to,
		Starters: []Starter{{Key: "dst", StartPoints: 2, FinalPoints: 7}},
	})
	if len(timeline.Changes) != 0 {
		t.Fatalf("uncredited residual changes = %+v, want none", timeline.Changes)
	}
	final := timeline.Samples[len(timeline.Samples)-1]
	if final.ScoreA != 7 || !final.At.Equal(minuteBucket(to)) {
		t.Fatalf("window residual sample = %+v, want score 7 at To's minute bucket", final)
	}
}

func TestBuildAnchorsUncreditedDefenseAtMatchedGameEnd(t *testing.T) {
	gameEnd := time.Date(2026, time.October, 9, 18, 0, 0, 0, time.UTC)
	laterGameEnd := gameEnd.Add(90 * time.Minute)
	timeline := Build(Input{
		Starters: []Starter{{Key: "dst", ProTeam: "KC", FinalPoints: 7}},
		Games: []Game{{
			HomeTeam: "KC",
			Plays: []fantasy.NFLPlay{
				{At: gameEnd.Add(-time.Hour), Period: 1, Kind: fantasy.NFLPlayOther},
				{At: gameEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
			},
		}, {
			HomeTeam: "SF",
			Plays: []fantasy.NFLPlay{
				{At: gameEnd.Add(time.Hour), Period: 1, Kind: fantasy.NFLPlayOther},
				{At: laterGameEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
			},
		}},
	})
	if len(timeline.Changes) != 0 {
		t.Fatalf("D/ST residual changes = %+v, want no credited change", timeline.Changes)
	}
	final := sampleAt(t, timeline, gameEnd.Truncate(time.Minute))
	if final.ScoreA != 7 {
		t.Errorf("D/ST end sample = %+v, want score 7", final)
	}
	for _, sample := range timeline.Samples {
		if sample.At.Equal(laterGameEnd.Truncate(time.Minute)) {
			t.Errorf("D/ST residual was delayed to the later game: %+v", sample)
		}
	}
}

func TestBuildWindowAnchorsUncreditedResidualAtGameEnd(t *testing.T) {
	from := time.Date(2026, time.October, 9, 13, 0, 0, 0, time.UTC)
	gameEnd := from.Add(5 * time.Minute)
	to := from.Add(10 * time.Minute)
	timeline := Build(Input{
		From: from,
		To:   to,
		Starters: []Starter{{
			Key: "dst", ProTeam: "KC", FinalPoints: 7,
		}},
		Games: []Game{{
			HomeTeam: "KC",
			Plays: []fantasy.NFLPlay{
				{At: from.Add(time.Minute), Period: 1, Kind: fantasy.NFLPlayOther},
				{At: gameEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
			},
		}},
	})
	if len(timeline.Changes) != 0 {
		t.Fatalf("uncredited window residual changes = %+v, want none", timeline.Changes)
	}
	if sample := sampleAt(t, timeline, gameEnd); sample.ScoreA != 7 {
		t.Errorf("game-end residual sample = %+v, want score 7", sample)
	}
}

func TestBuildWindowWithoutEndOfGameAnchorsResidualAtTo(t *testing.T) {
	from := time.Date(2026, time.October, 9, 13, 0, 0, 0, time.UTC)
	to := from.Add(5*time.Minute + 30*time.Second)
	timeline := Build(Input{
		From:     from,
		To:       to,
		Starters: []Starter{{Key: "rb", AthleteID: "rb", ProTeam: "KC", FinalPoints: 7}},
		Games: []Game{{
			HomeTeam: "KC",
			Plays: []fantasy.NFLPlay{{
				At: from.Add(time.Minute), Kind: fantasy.NFLPlayOther,
				Participants: []fantasy.NFLPlayParticipant{{AthleteID: "rb", Role: "rusher"}},
			}},
		}},
	})
	if len(timeline.Changes) != 0 {
		t.Fatalf("uncredited running-game residual changes = %+v, want none", timeline.Changes)
	}
	if sample := sampleAt(t, timeline, minuteBucket(to)); sample.ScoreA != 7 {
		t.Errorf("To residual sample = %+v, want score 7", sample)
	}
}

func TestBuildWindowAnchorsEndedGameAtFirstBucketAfterFrom(t *testing.T) {
	from := time.Date(2026, time.October, 9, 13, 10, 0, 0, time.UTC)
	gameEnd := from.Add(-5 * time.Minute)
	timeline := Build(Input{
		From: from,
		To:   from.Add(5 * time.Minute),
		Starters: []Starter{{
			Key: "dst", ProTeam: "KC", FinalPoints: 7,
		}},
		Games: []Game{{
			HomeTeam: "KC",
			Plays: []fantasy.NFLPlay{
				{At: gameEnd.Add(-time.Minute), Period: 1, Kind: fantasy.NFLPlayOther},
				{At: gameEnd, Period: 4, Kind: fantasy.NFLPlayEndOfGame},
			},
		}},
	})
	if len(timeline.Changes) != 0 {
		t.Fatalf("ended-game residual changes = %+v, want none", timeline.Changes)
	}
	firstBucket := minuteBucket(from).Add(time.Minute)
	if sample := sampleAt(t, timeline, firstBucket); sample.ScoreA != 7 {
		t.Errorf("post-From residual sample = %+v, want score 7", sample)
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

func sampleAt(t *testing.T, timeline Timeline, at time.Time) Sample {
	t.Helper()
	for _, sample := range timeline.Samples {
		if sample.At.Equal(at) {
			return sample
		}
	}
	t.Fatalf("no sample at %s in %+v", at, timeline.Samples)
	return Sample{}
}
