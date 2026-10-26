import {
  GameState,
  MatchupStatus,
  type LineupPlayer,
  type LiveGame,
  type MatchupDetail,
  type MatchupLineup,
} from "@/gen/leaguelens/v1/matchups_pb"
import { InjuryStatus } from "@/gen/leaguelens/v1/players_pb"
import { type TeamRecord } from "@/gen/leaguelens/v1/leagues_pb"
import { kickoffTime } from "@/components/overview/format"

export type ScoreTone = "winning" | "losing"

export type RoundStatus = "Bye" | "Upcoming" | "In progress" | "Final"

export type LineupPairRow = {
  slot: string
  mine?: LineupPlayer
  theirs?: LineupPlayer
}

function slotGroups(players: LineupPlayer[]) {
  const groups = new Map<string, LineupPlayer[]>()
  for (const player of players) {
    const group = groups.get(player.slot) ?? []
    group.push(player)
    groups.set(player.slot, group)
  }
  return groups
}

function appendRemainingSlots(order: string[], slots: string[], start: number) {
  for (let index = start; index < slots.length; index++) {
    order.push(slots[index])
  }
}

// Merges two slot lists that follow the same server order into one, so a slot
// only one side filled still lands in its usual place.
function mergeSlotOrder(mine: string[], theirs: string[]) {
  const mineSlots = new Set(mine)
  const theirSlots = new Set(theirs)
  const order: string[] = []
  let mineIndex = 0
  let theirIndex = 0

  while (mineIndex < mine.length && theirIndex < theirs.length) {
    const mineSlot = mine[mineIndex]
    const theirSlot = theirs[theirIndex]
    if (mineSlot === theirSlot) {
      order.push(mineSlot)
      mineIndex++
      theirIndex++
    } else if (!theirSlots.has(mineSlot)) {
      order.push(mineSlot)
      mineIndex++
    } else if (!mineSlots.has(theirSlot)) {
      order.push(theirSlot)
      theirIndex++
    } else {
      order.push(mineSlot)
      mineIndex++
    }
  }

  appendRemainingSlots(order, mine, mineIndex)
  appendRemainingSlots(order, theirs, theirIndex)
  return order
}

function pairSlotRows(
  slot: string,
  mine: LineupPlayer[] = [],
  theirs: LineupPlayer[] = [],
): LineupPairRow[] {
  const rows: LineupPairRow[] = []
  const rowCount = Math.max(mine.length, theirs.length)
  for (let index = 0; index < rowCount; index++) {
    rows.push({ slot, mine: mine[index], theirs: theirs[index] })
  }
  return rows
}

/**
 * Pairs each side's players by slot, preserving server order within each slot.
 * Slots merge in server order so an empty slot on one side does not shift every
 * subsequent player into the wrong row.
 */
export function pairLineupRows(
  mine: LineupPlayer[],
  theirs: LineupPlayer[],
): LineupPairRow[] {
  const mineGroups = slotGroups(mine)
  const theirGroups = slotGroups(theirs)
  const slots = mergeSlotOrder(
    Array.from(mineGroups.keys()),
    Array.from(theirGroups.keys()),
  )
  const rows: LineupPairRow[] = []
  for (const slot of slots) {
    rows.push(
      ...pairSlotRows(slot, mineGroups.get(slot), theirGroups.get(slot)),
    )
  }
  return rows
}

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

// A starter's unit on the field in a live game, and whether the drive is in
// the red zone.
export type FieldState = { unit: "offense" | "defense"; redZone: boolean }

/**
 * Whether a starter's unit is on the field right now: offense while their NFL
 * team has the ball, a D/ST while the other team does. Undefined between
 * plays with no possession (breaks, halftime) or when their game isn't live.
 */
export function fieldState(
  player: LineupPlayer,
  games: LiveGame[],
): FieldState | undefined {
  if (!player.proTeam || player.gameState !== GameState.KICKED_OFF) return
  const game = games.find(
    (g) => g.homeTeam === player.proTeam || g.awayTeam === player.proTeam,
  )
  if (!game?.possessionTeam) return
  const hasBall = game.possessionTeam === player.proTeam
  const teamDefense = player.slot === "DEF"
  if (hasBall === teamDefense) return
  return { unit: teamDefense ? "defense" : "offense", redZone: game.redZone }
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

/**
 * Whether the owner won the round. Only finished rounds get a tone, so the
 * header score stays neutral while a week is upcoming or in progress.
 */
export function ownerScoreTone(
  team: MatchupLineup,
  opponent: MatchupLineup | undefined,
  status: RoundStatus,
): ScoreTone | undefined {
  if (status !== "Final") return undefined
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

/**
 * Points each side is heading for: the final score once the round is over,
 * otherwise the projection (actuals for kicked-off starters plus projections).
 */
export function expectedScore(side: MatchupLineup, status: RoundStatus) {
  if (status === "Final") return side.score
  return side.projectedScore ?? teamScore(side, status)
}

/**
 * The owner's share of both sides' expected points, from 0 to 1. Undefined
 * when either side has nothing to compare yet.
 */
export function ownerShare(
  team: MatchupLineup,
  opponent: MatchupLineup,
  status: RoundStatus,
) {
  const mine = expectedScore(team, status)
  const theirs = expectedScore(opponent, status)
  if (mine === undefined || theirs === undefined || mine + theirs <= 0)
    return undefined
  return mine / (mine + theirs)
}

/**
 * Whether the owner won a lineup row. Only finished rounds get a tone, so
 * upcoming and in-progress weeks stay neutral. Rows where either player has
 * no points (bye, did not play) and ties have no tone.
 */
export function ownerRowTone(
  status: RoundStatus,
  mine?: LineupPlayer,
  theirs?: LineupPlayer,
): ScoreTone | undefined {
  if (status !== "Final" || !mine || !theirs) return undefined
  const minePoints = shownPoints(mine)
  const theirPoints = shownPoints(theirs)
  if (minePoints === undefined || theirPoints === undefined) return undefined
  if (minePoints > theirPoints) return "winning"
  if (minePoints < theirPoints) return "losing"
  return undefined
}
