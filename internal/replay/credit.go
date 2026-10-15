package replay

import "github.com/adityachoudhari26/league-lens/internal/fantasy"

type playerDelta struct {
	points     float64
	adjustment float64
	touchdowns int
	fieldGoals int
	credited   bool
}

func scoringPoints(rules []fantasy.ScoringRule) map[fantasy.Stat]float64 {
	points := make(map[fantasy.Stat]float64, len(rules))
	for _, rule := range rules {
		perUnits := rule.PerUnits
		if perUnits == 0 {
			perUnits = 1
		}
		points[rule.Stat] += rule.Points / perUnits
	}
	return points
}

// gameForStarter matches a starter to a game by athlete participation, then by team.
func gameForStarter(starter Starter, games []gameTimeline) int {
	if starter.AthleteID != "" {
		for gameIndex, game := range games {
			for _, play := range game.game.Plays {
				for _, participant := range play.Participants {
					if participant.AthleteID == starter.AthleteID {
						return gameIndex
					}
				}
			}
		}
	}
	if starter.ProTeam != "" {
		for gameIndex, game := range games {
			if game.game.HomeTeam == starter.ProTeam || game.game.AwayTeam == starter.ProTeam {
				return gameIndex
			}
		}
	}
	return -1
}

// creditPlay applies the supported scoring events for one play to one athlete.
func creditPlay(play fantasy.NFLPlay, athleteID string, rulePoints map[fantasy.Stat]float64) playerDelta {
	delta := playerDelta{}
	switch play.Kind {
	case fantasy.NFLPlayRush, fantasy.NFLPlayRushingTouchdown:
		rusher := participantID(play, "rusher")
		if rusher == athleteID {
			addStat(&delta, fantasy.StatRushingYards, float64(play.Yards), rulePoints)
		}
		if play.Kind == fantasy.NFLPlayRushingTouchdown {
			scorer := participantID(play, "scorer")
			if scorer == "" {
				scorer = rusher
			}
			if scorer == athleteID {
				addStat(&delta, fantasy.StatRushingTouchdowns, 1, rulePoints)
				delta.touchdowns++
			}
		}
	case fantasy.NFLPlayPassReception, fantasy.NFLPlayPassingTouchdown:
		passer := participantID(play, "passer")
		receiver := participantID(play, "receiver")
		if receiver == "" {
			receiver = participantID(play, "scorer")
		}
		if passer == athleteID {
			addStat(&delta, fantasy.StatPassingYards, float64(play.Yards), rulePoints)
		}
		if receiver == athleteID {
			addStat(&delta, fantasy.StatReceivingYards, float64(play.Yards), rulePoints)
			addStat(&delta, fantasy.StatReceptions, 1, rulePoints)
		}
		if play.Kind == fantasy.NFLPlayPassingTouchdown {
			if passer == athleteID {
				addStat(&delta, fantasy.StatPassingTouchdowns, 1, rulePoints)
				delta.touchdowns++
			}
			scorer := participantID(play, "scorer")
			if scorer == "" {
				scorer = receiver
			}
			if scorer == athleteID {
				addStat(&delta, fantasy.StatReceivingTouchdowns, 1, rulePoints)
				delta.touchdowns++
			}
		}
	case fantasy.NFLPlayInterception:
		if participantID(play, "passer") == athleteID {
			addStat(&delta, fantasy.StatInterceptions, 1, rulePoints)
		}
	case fantasy.NFLPlayFieldGoalGood:
		if participantID(play, "kicker") == athleteID {
			delta.points += 3
			delta.fieldGoals++
			delta.credited = true
		}
	}

	if play.ExtraPointGood &&
		(play.Kind == fantasy.NFLPlayRushingTouchdown || play.Kind == fantasy.NFLPlayPassingTouchdown) {
		kicker := participantID(play, "kicker")
		if kicker == "" {
			kicker = participantID(play, "patScorer")
		}
		if kicker == athleteID {
			delta.points++
			delta.credited = true
		}
	}
	return delta
}

func participantID(play fantasy.NFLPlay, role string) string {
	for _, participant := range play.Participants {
		if participant.Role == role {
			return participant.AthleteID
		}
	}
	return ""
}

func addStat(delta *playerDelta, stat fantasy.Stat, value float64, rulePoints map[fantasy.Stat]float64) {
	delta.points += value * rulePoints[stat]
	delta.credited = true
}

func addDelta(before, after playerDelta) playerDelta {
	return playerDelta{
		points:     before.points + after.points,
		adjustment: before.adjustment + after.adjustment,
		touchdowns: before.touchdowns + after.touchdowns,
		fieldGoals: before.fieldGoals + after.fieldGoals,
		credited:   before.credited || after.credited,
	}
}
