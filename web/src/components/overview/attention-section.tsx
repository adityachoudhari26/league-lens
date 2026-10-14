import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { CircleAlert, ChevronDown } from "lucide-react"
import { type LineupAlert } from "@/gen/leaguelens/v1/overview_pb"
import { overviewQueries } from "@/lib/api/overview"
import { Button } from "@/components/ui/button"
import { Section, SectionLoading, SectionError, SectionMessage, CoverageNote } from "./section"
import { observedTime } from "./format"

function AlertCard({ alert }: { alert: LineupAlert }) {
  return <details className="group rounded-lg border border-amber-200 bg-amber-50/60 dark:border-[#514636] dark:bg-[#2b2926]">
    <summary className="flex cursor-pointer list-none items-start gap-3 rounded-lg p-4 focus-visible:outline-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
      <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-amber-700 dark:text-[#d8b77b]" />
      <div className="min-w-0 flex-1"><p className="mb-1 text-xs text-muted-foreground">{alert.team?.provider.toUpperCase()} · {alert.team?.leagueName}</p><h3 className="text-sm font-medium">{alert.title}</h3><p className="mt-1 text-xs text-muted-foreground">{alert.team?.name} · Observed {observedTime(alert.team?.observedAt)}</p></div>
      <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-muted-foreground group-open:rotate-180" />
    </summary><p className="border-t p-4 text-sm leading-6 text-muted-foreground">{alert.detail}</p>
  </details>
}

export function AttentionSection() {
  const query = useQuery(overviewQueries.attention)
  const [expanded, setExpanded] = useState(false)
  const alerts = query.data?.alerts ?? []
  const shown = expanded ? alerts : alerts.slice(0, 3)
  const hidden = alerts.slice(3)
  const urgent = hidden.filter((alert) => alert.severity === "action").length

  return <Section title="Needs attention" count={query.data ? alerts.length : undefined}>
    {query.isPending ? <SectionLoading /> : query.isError ? <SectionError retry={() => { void query.refetch() }} /> : <>
      {alerts.length ? <div className="grid gap-3">{shown.map((alert) => <AlertCard key={alert.id} alert={alert} />)}</div> : <SectionMessage>{query.data.coverage?.state === "empty" ? "Import a league to check your lineup." : "No empty starting slots detected in the available data. Other lineup issues have not been checked."}</SectionMessage>}
      {alerts.length > 3 && <Button variant="ghost" size="sm" className="mt-2" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "Show less" : `Show ${hidden.length} more${urgent ? ` · ${urgent} need action` : ""}`}</Button>}
      <CoverageNote coverage={query.data.coverage} />
    </>}
  </Section>
}
