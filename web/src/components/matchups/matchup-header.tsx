import {
  ownerShare,
  type RoundStatus,
  type ScoreTone,
} from "@/components/matchups/rules"
import { type MatchupLineup } from "@/gen/leaguelens/v1/matchups_pb"

const statusToneClass: Record<RoundStatus, string> = {
  Upcoming: "bg-sky-100 text-sky-700 dark:bg-[#23303a] dark:text-[#9bbfd9]",
  "In progress":
    "bg-amber-100 text-amber-700 dark:bg-[#4a3b20] dark:text-[#f5d08a]",
  Final: "bg-stone-200 text-stone-700 dark:bg-[#302e2b] dark:text-[#c4bdb4]",
  Bye: "bg-stone-200 text-stone-700 dark:bg-[#302e2b] dark:text-[#c4bdb4]",
}

const finalToneClass: Record<ScoreTone, string> = {
  winning: "bg-teal-100 text-teal-700 dark:bg-[#23332f] dark:text-[#9bc8bf]",
  losing: "bg-red-100 text-red-700 dark:bg-[#3a2a2b] dark:text-[#d79b97]",
}

// Final rounds take the owner's result color; live rounds get a pulsing dot.
export function StatusPill({
  status,
  tone,
}: {
  status: RoundStatus
  tone?: ScoreTone
}) {
  const toneClass =
    status === "Final" && tone ? finalToneClass[tone] : statusToneClass[status]
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${toneClass}`}
    >
      {status === "In progress" && (
        <span className="size-1.5 animate-pulse rounded-full bg-current" />
      )}
      {status}
    </span>
  )
}

// Splits the bar by each side's expected points; the owner's side is teal.
export function ScoreShareBar({
  team,
  opponent,
  status,
}: {
  team: MatchupLineup
  opponent: MatchupLineup
  status: RoundStatus
}) {
  const share = ownerShare(team, opponent, status)
  if (share === undefined) return null
  return (
    <div className="flex h-1.5 gap-0.5 overflow-hidden rounded-full">
      <div
        className="rounded-l-full bg-teal-600/80 dark:bg-[#9bc8bf]/80"
        style={{ width: `${share * 100}%` }}
      />
      <div className="flex-1 rounded-r-full bg-stone-300 dark:bg-[#4a4642]" />
    </div>
  )
}
