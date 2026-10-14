import { scoreLabel } from "@/components/overview/format"
import { Lineup } from "@/components/matchups/lineup"
import {
  ownerScoreTone,
  recordLabel,
  roundStatus,
  teamScore,
  type RoundStatus,
  type ScoreTone,
} from "@/components/matchups/rules"
import {
  type MatchupDetail,
  type MatchupLineup,
} from "@/gen/leaguelens/v1/dashboard_pb"

const scoreToneClass: Record<ScoreTone, string> = {
  winning: "text-teal-700 dark:text-[#9bc8bf]",
  losing: "text-red-700 dark:text-[#d79b97]",
}

function TeamScore({
  side,
  label,
  status,
  mirrored,
  tone,
}: {
  side: MatchupLineup
  label: string
  status: RoundStatus
  mirrored?: boolean
  tone?: ScoreTone
}) {
  const remaining = side.startersLeftToPlay
  const summary = [
    recordLabel(side.record),
    remaining ? `${remaining} left to play` : "All played",
  ].filter(Boolean)
  return (
    <div className={mirrored ? "text-right" : undefined}>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-0.5 truncate text-sm font-medium">{side.teamName}</p>
      <p
        className={`mt-1 text-3xl font-semibold tabular-nums ${tone ? scoreToneClass[tone] : ""}`}
      >
        {scoreLabel(teamScore(side, status))}
      </p>
      <p className="mt-1 text-xs text-muted-foreground">
        {summary.join(" · ")}
      </p>
    </div>
  )
}

export function MatchupCard({
  matchup,
  team,
}: {
  matchup: MatchupDetail
  team: MatchupLineup
}) {
  const opponent = matchup.opponent
  const hasOpponent = opponent !== undefined
  const status = roundStatus(matchup)
  return (
    <section className="rounded-xl border bg-card">
      <div
        className={`grid items-center gap-4 px-4 py-4 ${hasOpponent ? "grid-cols-[1fr_auto_1fr]" : "grid-cols-[1fr_auto]"}`}
      >
        <TeamScore
          side={team}
          label="You"
          status={status}
          tone={ownerScoreTone(team, opponent, status)}
        />
        <span className="rounded-full border px-2.5 py-0.5 text-xs text-muted-foreground">
          {status}
        </span>
        {opponent && (
          <TeamScore
            side={opponent}
            label="Opponent"
            status={status}
            mirrored
          />
        )}
      </div>
      <div className="border-t">
        <Lineup
          mine={team.starters}
          theirs={opponent?.starters ?? []}
          hasOpponent={hasOpponent}
        />
      </div>
      <details className="group border-t">
        <summary className="cursor-pointer px-4 py-2.5 text-sm font-medium text-muted-foreground select-none hover:text-foreground">
          Bench ({team.bench.length}
          {opponent && ` · ${opponent.bench.length}`})
        </summary>
        <div className="border-t">
          <Lineup
            mine={team.bench}
            theirs={opponent?.bench ?? []}
            hasOpponent={hasOpponent}
          />
        </div>
      </details>
    </section>
  )
}
