import { Shield, type LucideProps } from "lucide-react"
import { scoreLabel } from "@/components/overview/format"
import {
  GameState,
  type LineupPlayer,
  type LiveGame,
} from "@/gen/leaguelens/v1/matchups_pb"
import {
  fieldState,
  gameDetail,
  injuryLabel,
  ownerRowTone,
  pairLineupRows,
  playerInfo,
  type FieldState,
  shownPoints,
  type RoundStatus,
} from "@/components/matchups/rules"
import { scoreToneClass } from "@/components/matchups/tones"
import { PlayerCardTrigger } from "@/components/player-card/player-card"

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

export function InjuryBadge({ status }: { status: string }) {
  const severe = status !== "Q"
  return (
    <span
      className={`shrink-0 rounded px-1 text-[10px] font-semibold ${severe ? "bg-red-100 text-red-700/80 dark:bg-[#4d3031] dark:text-[#f6a6a1]" : "bg-amber-100 text-amber-700/80 dark:bg-[#4a3b20] dark:text-[#f5d08a]"}`}
    >
      {status}
    </span>
  )
}

const fieldLabel = {
  offense: ["Has the ball", "In the red zone"],
  defense: ["On the field", "Defending the red zone"],
} as const

// Outside the red zone: a leather-brown football and a soft orange shield.
const fieldToneClass = {
  offense: "text-amber-800/75 dark:text-[#c79a72]",
  defense: "text-orange-500/80 dark:text-[#e5a36f]",
} as const

// A light tint filling the player's half of the row while their unit is on the
// field, red once the drive reaches the red zone.
function fieldTintClass(state?: FieldState) {
  if (!state) return ""
  if (state.redZone) return "bg-red-50 dark:bg-[#3a2627]"
  return state.unit === "offense"
    ? "bg-amber-50 dark:bg-[#322a20]"
    : "bg-orange-50 dark:bg-[#36281f]"
}

// A level, filled football with laces cut out in the card color. Lucide only
// has an angled rugby ball.
function FootballIcon({ className, ...props }: LucideProps) {
  return (
    <svg viewBox="0 0 24 24" className={className} {...props}>
      <path
        d="M1.5 12C5 3 19 3 22.5 12C19 21 5 21 1.5 12Z"
        fill="currentColor"
      />
      <path
        d="M8 12h8M10 10.25v3.5M12 10.25v3.5M14 10.25v3.5"
        className="stroke-card"
        strokeWidth={1.5}
        strokeLinecap="round"
        fill="none"
      />
    </svg>
  )
}

// A football while the player's offense has the ball, a shield while a D/ST is
// on the field; red once the drive reaches the red zone.
function FieldBadge({ state }: { state: FieldState }) {
  const Icon = state.unit === "offense" ? FootballIcon : Shield
  const label = fieldLabel[state.unit][state.redZone ? 1 : 0]
  return (
    <span title={label} className="shrink-0">
      <Icon
        aria-label={label}
        fill="currentColor"
        className={`size-3.5 ${state.redZone ? "text-red-700/80 dark:text-[#f09591]" : fieldToneClass[state.unit]}`}
      />
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

// A starting slot the team left unfilled, kept in its row so the other side
// stays aligned.
function EmptyPlayerCell({
  label,
  mirrored,
}: {
  label: string
  mirrored?: boolean
}) {
  return (
    <div
      className={`flex min-h-[52px] items-center px-3 py-2 text-sm text-muted-foreground dark:text-muted-foreground/70 ${mirrored ? "justify-end text-right" : ""}`}
    >
      {label}
    </div>
  )
}

function PlayerCell({
  player,
  mirrored,
  toneClass,
  liveGames,
  emptyLabel,
}: {
  player?: LineupPlayer
  mirrored?: boolean
  toneClass?: string
  liveGames: LiveGame[]
  emptyLabel?: string
}) {
  if (!player)
    return emptyLabel ? (
      <EmptyPlayerCell label={emptyLabel} mirrored={mirrored} />
    ) : (
      <div />
    )
  const injury = injuryLabel(player.injuryStatus)
  const field = fieldState(player, liveGames)
  return (
    <div
      className={`flex min-w-0 items-center gap-3 px-3 py-2 ${mirrored ? "flex-row-reverse text-right" : ""} ${fieldTintClass(field)}`}
    >
      <div className="min-w-0 flex-1">
        <div
          className={`flex items-center gap-1.5 ${mirrored ? "flex-row-reverse" : ""}`}
        >
          <PlayerCardTrigger
            playerId={player.playerId}
            className="text-sm font-medium"
          >
            {player.playerName}
          </PlayerCardTrigger>
          {injury && <InjuryBadge status={injury} />}
          {field && <FieldBadge state={field} />}
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
        className={`text-sm ${toneClass ? `font-semibold ${toneClass}` : "font-medium text-foreground"}`}
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
      className={`m-auto rounded px-1.5 py-0.5 text-[10px] font-semibold ${slotToneClass[slot] ?? "bg-muted text-muted-foreground"}`}
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
  liveGames,
  emptyLabel,
}: {
  slot: string
  mine?: LineupPlayer
  theirs?: LineupPlayer
  hasOpponent: boolean
  status: RoundStatus
  liveGames: LiveGame[]
  emptyLabel?: string
}) {
  const tone = ownerRowTone(status, mine, theirs)
  if (!hasOpponent)
    return (
      <div className="grid grid-cols-[1fr_3rem] items-stretch">
        <PlayerCell player={mine} liveGames={liveGames} />
        <SlotBadge slot={slot} />
      </div>
    )
  return (
    <div className="grid grid-cols-[1fr_3rem_1fr] items-stretch">
      <PlayerCell
        player={mine}
        toneClass={tone && scoreToneClass[tone]}
        liveGames={liveGames}
        emptyLabel={emptyLabel}
      />
      <SlotBadge slot={slot} />
      <PlayerCell
        player={theirs}
        mirrored
        liveGames={liveGames}
        emptyLabel={emptyLabel}
      />
    </div>
  )
}

/**
 * Rows of paired players. `liveGames` drives the on-field badges; bench rows
 * pass none because a bench D/ST can't be told apart from an offensive player.
 */
export function Lineup({
  mine,
  theirs,
  hasOpponent,
  status,
  liveGames = [],
  emptyLabel,
}: {
  mine: LineupPlayer[]
  theirs: LineupPlayer[]
  hasOpponent: boolean
  status: RoundStatus
  liveGames?: LiveGame[]
  emptyLabel?: string
}) {
  const rows = pairLineupRows(mine, theirs)
  return (
    <div className="divide-y">
      {rows.map((row, index) => (
        <LineupRow
          key={`${row.slot}-${index}`}
          slot={row.slot}
          mine={row.mine}
          theirs={row.theirs}
          hasOpponent={hasOpponent}
          status={status}
          liveGames={liveGames}
          emptyLabel={emptyLabel}
        />
      ))}
    </div>
  )
}
