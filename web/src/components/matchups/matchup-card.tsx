import { useQuery } from "@tanstack/react-query"
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
import { MatchupWinChancePanels } from "@/components/matchups/win-chance"
import { matchupQueries } from "@/lib/api/matchups"
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

const LIVE_GAMES_POLL_MS = 15_000

// Possession for the NFL games in progress, polled only while the round is.
function useLiveGames(status: RoundStatus) {
  const live = status === "In progress"
  const query = useQuery({
    ...matchupQueries.liveGames,
    enabled: live,
    refetchInterval: live ? LIVE_GAMES_POLL_MS : false,
  })
  return live ? query.data : undefined
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
  const liveGames = useLiveGames(status)
  return (
    <div className="@container">
      <div
        className={`grid gap-4 ${hasOpponent ? "@4xl:grid-cols-[minmax(0,7fr)_minmax(0,5fr)] @4xl:items-start" : ""}`}
      >
        <section className="rounded-xl border bg-card @4xl:col-start-1 @4xl:row-start-1">
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
                <ScoreShareBar
                  team={team}
                  opponent={opponent}
                  status={status}
                />
              </div>
            )}
          </div>
        </section>
        {hasOpponent && (
          <aside className="@4xl:sticky @4xl:top-4 @4xl:col-start-2 @4xl:row-span-2 @4xl:row-start-1">
            <MatchupWinChancePanels key={matchup.id} matchup={matchup} />
          </aside>
        )}
        <section className="overflow-hidden rounded-xl border bg-card @4xl:col-start-1 @4xl:row-start-2">
          <Lineup
            mine={team.starters}
            theirs={opponent?.starters ?? []}
            hasOpponent={hasOpponent}
            status={status}
            liveGames={liveGames}
          />
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
      </div>
    </div>
  )
}
