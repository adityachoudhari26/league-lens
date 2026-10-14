import { useQuery } from "@tanstack/react-query"
import { type OverviewMatchup } from "@/gen/leaguelens/v1/overview_pb"
import { overviewQueries } from "@/lib/api/overview"
import { Section, SectionLoading, SectionError, SectionMessage, CoverageNote } from "./section"
import { observedTime, scoreLabel } from "./format"

function MatchupCard({ matchup }: { matchup: OverviewMatchup }) {
  const team = matchup.team
  const espn = team?.provider === "espn"
  const opponent = matchup.isBye ? "Bye" : matchup.opponentName || "Opponent unavailable"
  return <article className={`rounded-lg border border-t-2 bg-card p-4 ${espn ? "border-t-rose-400 dark:border-t-[#946570]" : "border-t-violet-400 dark:border-t-[#84759f]"}`}>
    <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-sm font-medium">{team?.leagueName}</h3><span className={`rounded px-2 py-1 text-[10px] font-semibold ${espn ? "bg-rose-50 text-rose-700 dark:bg-[#382c33] dark:text-[#d3a4af]" : "bg-violet-50 text-violet-700 dark:bg-[#322e3e] dark:text-[#bfb0d8]"}`}>{team?.provider.toUpperCase()}</span></div>
    <p className="mt-1 text-xs text-muted-foreground">{team?.season} season{team?.scoringPeriod !== undefined ? ` · Week ${team.scoringPeriod}` : ""} · {matchup.status.replaceAll("_", " ")}</p>
    <div className="mt-4 grid grid-cols-[1fr_auto_1fr] items-center gap-3"><div><p className="text-xs text-muted-foreground">You</p><p className="mt-1 text-sm font-medium">{team?.name}</p><p className="mt-2 text-2xl font-semibold tabular-nums text-teal-700 dark:text-[#9bc8bf]">{scoreLabel(matchup.score)}</p></div><span className="text-xs text-muted-foreground">vs</span><div className="text-right"><p className="text-xs text-muted-foreground">Opponent</p><p className="mt-1 text-sm font-medium">{opponent}</p><p className="mt-2 text-2xl font-semibold tabular-nums">{scoreLabel(matchup.opponentScore)}</p></div></div>
    <p className="mt-4 border-t pt-3 text-xs text-muted-foreground">{matchup.fetchedAt ? `Last imported ${observedTime(matchup.fetchedAt)}` : "No imported matchup for this scoring week."}</p>
  </article>
}

export function MatchupsSection() {
  const query = useQuery(overviewQueries.matchups)
  return <Section title="Matchups" count={query.data?.matchups.length}>
    {query.isPending ? <SectionLoading /> : query.isError ? <SectionError retry={() => { void query.refetch() }} /> : <>
      {query.data.matchups.length ? <div className="grid gap-3">{query.data.matchups.map((matchup) => <MatchupCard key={matchup.team?.id} matchup={matchup} />)}</div> : <SectionMessage>No imported matchups yet. Import a league to get started.</SectionMessage>}
      <CoverageNote coverage={query.data.coverage} />
    </>}
  </Section>
}
