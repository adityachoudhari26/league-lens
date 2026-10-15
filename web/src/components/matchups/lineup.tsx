import { scoreLabel } from "@/components/overview/format"
import { GameState, type LineupPlayer } from "@/gen/leaguelens/v1/dashboard_pb"
import {
  gameDetail,
  injuryLabel,
  ownerRowTone,
  playerInfo,
  shownPoints,
  type RoundStatus,
} from "@/components/matchups/rules"
import { scoreToneClass } from "@/components/matchups/tones"

const slotToneClass: Record<string, string> = {
  QB: "bg-rose-100 text-rose-700 dark:bg-[#3a2a2f] dark:text-[#d99aa8]",
  RB: "bg-teal-100 text-teal-700 dark:bg-[#23332f] dark:text-[#9bc8bf]",
  WR: "bg-sky-100 text-sky-700 dark:bg-[#23303a] dark:text-[#9bbfd9]",
  TE: "bg-orange-100 text-orange-700 dark:bg-[#3a2c22] dark:text-[#dba67f]",
  FLEX: "bg-violet-100 text-violet-700 dark:bg-[#2e2a3a] dark:text-[#b9a8d9]",
  SUPERFLEX:
    "bg-violet-100 text-violet-700 dark:bg-[#2e2a3a] dark:text-[#b9a8d9]",
  K: "bg-lime-100 text-lime-700 dark:bg-[#2b3322] dark:text-[#b5cc8e]",
  DEF: "bg-stone-200 text-stone-700 dark:bg-[#302e2b] dark:text-[#c4bdb4]",
}

// Short labels for slots that don't fit the badge column.
const slotLabel: Record<string, string> = {
  BENCH: "BN",
  SUPERFLEX: "SFLX",
  UNKNOWN: "—",
}

function InjuryBadge({ status }: { status: string }) {
  const severe = status !== "Q"
  return (
    <span
      className={`shrink-0 rounded px-1 text-[10px] font-semibold ${severe ? "bg-red-100 text-red-700/80 dark:bg-[#4d3031] dark:text-[#f6a6a1]" : "bg-amber-100 text-amber-700/80 dark:bg-[#4a3b20] dark:text-[#f5d08a]"}`}
    >
      {status}
    </span>
  )
}

function PlayerSubtitle({ player }: { player: LineupPlayer }) {
  const info = playerInfo(player)
  const detail = gameDetail(player)
  const bye = player.gameState === GameState.BYE
  return (
    <p className="min-h-4 truncate text-xs text-muted-foreground">
      {info}
      {info && detail && " · "}
      {detail && (
        <span className={bye ? "text-red-600 dark:text-[#d79b97]" : undefined}>
          {detail}
        </span>
      )}
    </p>
  )
}

function PlayerCell({
  player,
  mirrored,
  toneClass,
}: {
  player?: LineupPlayer
  mirrored?: boolean
  toneClass?: string
}) {
  if (!player) return <div />
  const injury = injuryLabel(player.injuryStatus)
  return (
    <div
      className={`flex min-w-0 items-center gap-3 ${mirrored ? "flex-row-reverse text-right" : ""}`}
    >
      <div className="min-w-0 flex-1">
        <div
          className={`flex items-center gap-1.5 ${mirrored ? "flex-row-reverse" : ""}`}
        >
          <span className="truncate text-sm font-medium">
            {player.playerName}
          </span>
          {injury && <InjuryBadge status={injury} />}
        </div>
        <PlayerSubtitle player={player} />
      </div>
      <PlayerPoints player={player} mirrored={mirrored} toneClass={toneClass} />
    </div>
  )
}

// Points sit on top (0 before kickoff) with the projection underneath. A
// tone colors the points.
function PlayerPoints({
  player,
  mirrored,
  toneClass,
}: {
  player: LineupPlayer
  mirrored?: boolean
  toneClass?: string
}) {
  const projected = player.projectedPoints
  return (
    <div
      className={`w-12 shrink-0 tabular-nums ${mirrored ? "text-left" : "text-right"}`}
    >
      <p
        className={`text-sm ${toneClass ? `font-semibold ${toneClass}` : "text-foreground/70"}`}
      >
        {scoreLabel(shownPoints(player) ?? 0)}
      </p>
      {projected !== undefined && (
        <p className="text-[11px] text-muted-foreground">
          {scoreLabel(projected)}
        </p>
      )}
    </div>
  )
}

function SlotBadge({ slot }: { slot: string }) {
  return (
    <span
      className={`mx-auto rounded px-1.5 py-0.5 text-[10px] font-semibold ${slotToneClass[slot] ?? "bg-muted text-muted-foreground"}`}
    >
      {slotLabel[slot] ?? slot}
    </span>
  )
}

// Pairs players by row; on a bye there is no opponent column.
function LineupRow({
  slot,
  mine,
  theirs,
  hasOpponent,
  status,
}: {
  slot: string
  mine?: LineupPlayer
  theirs?: LineupPlayer
  hasOpponent: boolean
  status: RoundStatus
}) {
  const tone = ownerRowTone(status, mine, theirs)
  if (!hasOpponent)
    return (
      <div className="grid grid-cols-[1fr_3rem] items-center gap-2 px-3 py-2">
        <PlayerCell player={mine} />
        <SlotBadge slot={slot} />
      </div>
    )
  return (
    <div className="grid grid-cols-[1fr_3rem_1fr] items-center gap-2 px-3 py-2">
      <PlayerCell player={mine} toneClass={tone && scoreToneClass[tone]} />
      <SlotBadge slot={slot} />
      <PlayerCell player={theirs} mirrored />
    </div>
  )
}

export function Lineup({
  mine,
  theirs,
  hasOpponent,
  status,
}: {
  mine: LineupPlayer[]
  theirs: LineupPlayer[]
  hasOpponent: boolean
  status: RoundStatus
}) {
  const rows = Array.from({ length: Math.max(mine.length, theirs.length) })
  return (
    <div className="divide-y">
      {rows.map((_, index) => (
        <LineupRow
          key={index}
          slot={mine[index]?.slot ?? theirs[index]?.slot ?? ""}
          mine={mine[index]}
          theirs={theirs[index]}
          hasOpponent={hasOpponent}
          status={status}
        />
      ))}
    </div>
  )
}
