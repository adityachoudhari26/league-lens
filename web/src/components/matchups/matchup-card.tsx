import { scoreLabel } from "@/components/overview/format"
import { Lineup } from "@/components/matchups/lineup"
import { ScoreShareBar, StatusPill } from "@/components/matchups/matchup-header"
import {
  ownerScoreTone,
  recordLabel,
  roundStatus,
  teamScore,
  type RoundStatus,
  type ScoreTone,
} from "@/components/matchups/rules"
import { scoreToneClass } from "@/components/matchups/tones"
import {
  type MatchupDetail,
  type MatchupLineup,
} from "@/gen/leaguelens/v1/matchups_pb"

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
        {scoreLabel(teamScore(side, status) ?? 0)}
      </p>
      {side.projectedScore !== undefined && (
        <p className="text-sm text-muted-foreground tabular-nums">
          {scoreLabel(side.projectedScore)} proj
        </p>
      )}
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
  const tone = ownerScoreTone(team, opponent, status)
  return (
    <section className="rounded-xl border bg-card">
      <div
        className={`grid items-center gap-4 px-4 py-4 ${hasOpponent ? "grid-cols-[1fr_auto_1fr]" : "grid-cols-[1fr_auto]"}`}
      >
        <TeamScore side={team} label="You" status={status} tone={tone} />
        <StatusPill status={status} tone={tone} />
        {opponent && (
          <TeamScore
            side={opponent}
            label="Opponent"
            status={status}
            mirrored
          />
        )}
        {opponent && (
          <div className="col-span-3">
            <ScoreShareBar team={team} opponent={opponent} status={status} />
          </div>
        )}
      </div>
      <div className="border-t">
        <Lineup
          mine={team.starters}
          theirs={opponent?.starters ?? []}
          hasOpponent={hasOpponent}
          status={status}
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
            status={status}
          />
        </div>
      </details>
    </section>
  )
}
