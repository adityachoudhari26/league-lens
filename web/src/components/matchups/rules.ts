import {
  GameState,
  MatchupStatus,
  type LineupPlayer,
  type MatchupDetail,
  type MatchupLineup,
  type TeamRecord,
} from "@/gen/leaguelens/v1/dashboard_pb"
import { InjuryStatus } from "@/gen/leaguelens/v1/overview_pb"
import { kickoffTime } from "@/components/overview/format"

export type ScoreTone = "winning" | "losing"

export type RoundStatus = "Bye" | "Upcoming" | "In progress" | "Final"

/**
 * Derives the round's status from game states: providers store upcoming
 * rounds as UNKNOWN with a score of 0, so only FINAL is trusted as stored.
 */
export function roundStatus(matchup: MatchupDetail): RoundStatus {
  if (matchup.isBye) return "Bye"
  if (matchup.status === MatchupStatus.FINAL) return "Final"
  const starters = [matchup.team, matchup.opponent].flatMap(
    (side) => side?.starters ?? [],
  )
  return starters.some((player) => player.gameState === GameState.KICKED_OFF)
    ? "In progress"
    : "Upcoming"
}

/**
 * Points worth showing for a player. Unplayed games have none, even when the
 * provider reports 0 (Sleeper does).
 */
export function shownPoints(player: LineupPlayer) {
  return player.gameState === GameState.NOT_STARTED ||
    player.gameState === GameState.BYE
    ? undefined
    : player.points
}

export function teamScore(side: MatchupLineup, status: RoundStatus) {
  return status === "Upcoming" ? undefined : side.score
}

export function ownerScoreTone(
  team: MatchupLineup,
  opponent: MatchupLineup | undefined,
  status: RoundStatus,
): ScoreTone | undefined {
  const mine = teamScore(team, status)
  const theirs = opponent && teamScore(opponent, status)
  if (mine === undefined || theirs === undefined) return undefined
  if (mine > theirs) return "winning"
  if (mine < theirs) return "losing"
  return undefined
}

export function recordLabel(record?: TeamRecord) {
  if (!record) return undefined
  const ties = record.ties ? `-${record.ties}` : ""
  return `${record.wins}-${record.losses}${ties}`
}

export function injuryLabel(status: InjuryStatus) {
  switch (status) {
    case InjuryStatus.QUESTIONABLE:
      return "Q"
    case InjuryStatus.DOUBTFUL:
      return "D"
    case InjuryStatus.OUT:
      return "O"
    case InjuryStatus.INJURED_RESERVE:
      return "IR"
    default:
      return undefined
  }
}

// Kickoff time before the game, "Bye" on a bye week, nothing once it kicked off.
export function gameDetail(player: LineupPlayer) {
  if (player.gameState === GameState.BYE) return "Bye"
  if (player.gameState === GameState.NOT_STARTED)
    return kickoffTime(player.kickoffAt)
  return undefined
}

// Who the player is, shown before the game detail: the NFL team for now.
export function playerInfo(player: LineupPlayer) {
  return player.proTeam
}
