import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { type OverviewMatchup } from "@/gen/leaguelens/v1/overview_pb"
import { overviewQueries } from "@/lib/api/overview"
import {
  Section,
  SectionLoading,
  SectionError,
  SectionMessage,
} from "./section"
import { observedTime, scoreLabel } from "./format"

function MatchupCard({ matchup }: { matchup: OverviewMatchup }) {
  const team = matchup.team
  const espn = team?.provider === "espn"
  const opponent = matchup.isBye
    ? "Bye"
    : matchup.opponentName || "Opponent unavailable"
  const body = (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium">{team?.leagueName}</h3>
        <span
          className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${espn ? "bg-rose-50 text-rose-700 dark:bg-[#382c33] dark:text-[#d3a4af]" : "bg-violet-50 text-violet-700 dark:bg-[#322e3e] dark:text-[#bfb0d8]"}`}
        >
          {team?.provider.toUpperCase()}
        </span>
      </div>
      <p className="mt-0.5 text-xs text-muted-foreground">
        {team?.season} season
        {team?.scoringPeriod !== undefined
          ? ` · Week ${team.scoringPeriod}`
          : ""}{" "}
        · {matchup.status.replaceAll("_", " ")}
      </p>
      <div className="mt-3 grid grid-cols-[1fr_auto_1fr] items-center gap-2">
        <div>
          <p className="text-xs text-muted-foreground">You</p>
          <p className="mt-0.5 text-sm font-medium">{team?.name}</p>
          <p className="mt-1 text-xl font-semibold tabular-nums text-teal-700 dark:text-[#9bc8bf]">
            {scoreLabel(matchup.score)}
          </p>
        </div>
        <span className="text-xs text-muted-foreground">vs</span>
        <div className="text-right">
          <p className="text-xs text-muted-foreground">Opponent</p>
          <p className="mt-0.5 text-sm font-medium">{opponent}</p>
          <p className="mt-1 text-xl font-semibold tabular-nums">
            {scoreLabel(matchup.opponentScore)}
          </p>
        </div>
      </div>
      <p className="mt-3 border-t pt-2.5 text-xs text-muted-foreground">
        {matchup.fetchedAt
          ? `Last imported ${observedTime(matchup.fetchedAt)}`
          : "No imported matchup for this scoring week."}
      </p>
    </>
  )
  const cardClass = `rounded-lg border border-t-2 bg-card px-4 py-3.5 ${espn ? "border-t-rose-400 dark:border-t-[#946570]" : "border-t-violet-400 dark:border-t-[#84759f]"}`
  if (!team) return <article className={cardClass}>{body}</article>
  return (
    <Link
      to="/matchups"
      search={{ league: team.leagueId, week: matchup.matchupPeriod }}
      className={`block transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ${cardClass}`}
    >
      {body}
    </Link>
  )
}

export function MatchupsSection() {
  const query = useQuery(overviewQueries.matchups)
  return (
    <Section title="Matchups" count={query.data?.matchups.length}>
      {query.isPending ? (
        <SectionLoading />
      ) : query.isError ? (
        <SectionError
          retry={() => {
            void query.refetch()
          }}
        />
      ) : query.data.matchups.length ? (
        <div className="grid gap-3">
          {query.data.matchups.map((matchup) => (
            <MatchupCard key={matchup.team?.id} matchup={matchup} />
          ))}
        </div>
      ) : (
        <SectionMessage>
          No imported matchups yet. Import a league to get started.
        </SectionMessage>
      )}
    </Section>
  )
}
