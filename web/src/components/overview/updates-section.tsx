import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Activity, ArrowLeftRight, ChevronDown, Newspaper } from "lucide-react"
import { InjuryStatus, NewsCategory, type PlayerUpdate } from "@/gen/leaguelens/v1/overview_pb"
import { overviewQueries } from "@/lib/api/overview"
import { Button } from "@/components/ui/button"
import { Section, SectionLoading, SectionError, SectionMessage, CoverageNote } from "./section"
import { observedTime } from "./format"

function categoryPresentation(category: NewsCategory) {
  switch (category) {
    case NewsCategory.INJURY:
      return { label: "Injury", Icon: Activity, accent: "text-amber-700 dark:text-[#d8b77b]" }
    case NewsCategory.TRANSACTION:
      return { label: "Transaction", Icon: ArrowLeftRight, accent: "text-sky-700 dark:text-[#9db8cc]" }
    default:
      return { label: "News", Icon: Newspaper, accent: "text-muted-foreground" }
  }
}

function designatedStatus(status: InjuryStatus) {
  switch (status) {
    case InjuryStatus.QUESTIONABLE:
      return "Questionable"
    case InjuryStatus.DOUBTFUL:
      return "Doubtful"
    case InjuryStatus.OUT:
      return "Out"
    case InjuryStatus.INJURED_RESERVE:
      return "Injured reserve"
    default:
      return undefined
  }
}

function PlayerUpdateCard({ update }: { update: PlayerUpdate }) {
  const category = categoryPresentation(update.category)
  const status = designatedStatus(update.injuryStatus)
  const teams = update.teams.map((team) => `${team.provider.toUpperCase()} / ${team.leagueName}`).join(", ")

  return <details className="group rounded-lg border border-border bg-card">
    <summary className="flex cursor-pointer list-none items-start gap-3 rounded-lg p-4 focus-visible:outline-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
      <category.Icon aria-hidden="true" className={`mt-0.5 size-4 shrink-0 ${category.accent}`} />
      <div className="min-w-0 flex-1">
        <p className="mb-1 text-xs text-muted-foreground">
          <span className={`font-medium ${category.accent}`}>{category.label}</span>
          {status && <> · {status}</>}
          {teams && <> · {teams}</>}
        </p>
        <p className="text-sm font-semibold">{update.playerName}</p>
        <h3 className="mt-0.5 text-sm font-medium">{update.headline}</h3>
        {update.summary && <p className="mt-1 line-clamp-2 text-sm text-muted-foreground group-open:hidden">{update.summary}</p>}
      </div>
      <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-muted-foreground group-open:rotate-180" />
    </summary>
    <div className="border-t p-4 text-sm leading-6 text-muted-foreground">
      {update.summary && <p>{update.summary}</p>}
      <p className="mt-2 text-xs">{update.sourceLabel} · {observedTime(update.publishedAt)}</p>
      <a className="mt-2 inline-block text-sm font-medium text-primary underline-offset-4 hover:underline" href={update.sourceUrl} target="_blank" rel="noreferrer">
        Read on ESPN
      </a>
    </div>
  </details>
}

export function UpdatesSection() {
  const query = useQuery(overviewQueries.updates)
  const [expanded, setExpanded] = useState(false)
  const updates = query.data?.updates ?? []
  const shown = expanded ? updates : updates.slice(0, 5)
  const hidden = updates.slice(5)

  return <Section title="Player updates" count={query.data ? updates.length : undefined}>
    {query.isPending ? <SectionLoading /> : query.isError ? <SectionError retry={() => { void query.refetch() }} /> : <>
      {updates.length ? <div className="grid gap-3">{shown.map((update) => <PlayerUpdateCard key={update.id} update={update} />)}</div> : <SectionMessage>No recent news for your players.</SectionMessage>}
      {updates.length > 5 && <Button variant="ghost" size="sm" className="mt-2" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "Show less" : `Show ${hidden.length} more`}</Button>}
      <CoverageNote coverage={query.data.coverage} />
    </>}
  </Section>
}
