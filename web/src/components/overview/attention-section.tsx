import { useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { CircleAlert, ChevronDown, Eye, X } from "lucide-react"
import {
  AlertSeverity,
  type LineupAlert,
} from "@/gen/leaguelens/v1/overview_pb"
import { overviewQueries, useDismissLineupAlert } from "@/lib/api/overview"
import { Button } from "@/components/ui/button"
import {
  Section,
  SectionLoading,
  SectionError,
  SectionMessage,
} from "./section"
import { kickoffTime, observedTime } from "./format"

const severityStyles = {
  action: {
    label: "Action needed",
    Icon: CircleAlert,
    card: "border-amber-200/80 bg-amber-50/50 dark:border-[#4a4234] dark:bg-[#27251f]",
    accent: "text-amber-700 dark:text-[#d8b77b]",
  },
  monitor: {
    label: "Monitor",
    Icon: Eye,
    card: "border-border bg-card",
    accent: "text-sky-700 dark:text-[#9db8cc]",
  },
}

function AlertCard({
  alert,
  onDismiss,
}: {
  alert: LineupAlert
  onDismiss: (alert: LineupAlert) => void
}) {
  const style =
    alert.severity === AlertSeverity.ACTION
      ? severityStyles.action
      : severityStyles.monitor
  const kickoff = kickoffTime(alert.kickoffAt)
  return (
    <div className="relative">
      <details className={`group rounded-lg border ${style.card}`}>
        <summary className="flex cursor-pointer list-none items-start gap-2.5 rounded-lg px-3 py-2.5 pr-10 focus-visible:outline-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
          <style.Icon
            aria-hidden="true"
            className={`mt-0.5 size-4 shrink-0 ${style.accent}`}
          />
          <div className="min-w-0 flex-1">
            <p className="truncate text-xs text-muted-foreground">
              <span className={`font-medium ${style.accent}`}>
                {style.label}
              </span>{" "}
              · {alert.team?.leagueName}
            </p>
            <h3 className="mt-0.5 text-sm">
              <span className="font-medium">{alert.title}</span>
              <span className="text-foreground/80">
                {" "}
                · {alert.playerName || alert.team?.name}
              </span>
            </h3>
          </div>
          <ChevronDown
            aria-hidden="true"
            className="mt-0.5 size-4 shrink-0 text-muted-foreground group-open:rotate-180"
          />
        </summary>
        <div className="border-t px-3 py-2.5 text-sm leading-6 text-muted-foreground">
          <p>{alert.detail}</p>
          <p className="mt-1 text-xs">
            {alert.team?.name} · Synced {observedTime(alert.team?.observedAt)}
            {kickoff ? ` · Kickoff ${kickoff}` : ""}
          </p>
        </div>
      </details>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="absolute right-1.5 top-1.5"
        aria-label={`Dismiss: ${alert.title}`}
        onClick={() => onDismiss(alert)}
      >
        <X aria-hidden="true" className="size-4" />
      </Button>
    </div>
  )
}

function DismissedNotice({
  alert,
  onUndo,
}: {
  alert: LineupAlert
  onUndo: (alert: LineupAlert) => void
}) {
  return (
    <div className="mb-3 flex min-w-0 items-center justify-between gap-2 rounded-lg border border-border px-3 py-1.5 text-xs text-muted-foreground">
      <p className="truncate">
        Dismissed · {alert.title} · {alert.playerName || alert.team?.name}
      </p>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => onUndo(alert)}
      >
        Undo
      </Button>
    </div>
  )
}

export function AttentionSection() {
  const query = useQuery(overviewQueries.attention)
  const [expanded, setExpanded] = useState(false)
  const [dismissedAlert, setDismissedAlert] = useState<LineupAlert | null>(null)
  const dismissTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  )
  const dismissal = useDismissLineupAlert()
  const alerts = query.data?.alerts ?? []
  const shown = expanded ? alerts : alerts.slice(0, 3)
  const hidden = alerts.slice(3)
  const urgent = hidden.filter(
    (alert) => alert.severity === AlertSeverity.ACTION,
  ).length

  useEffect(
    () => () => {
      if (dismissTimer.current !== undefined) clearTimeout(dismissTimer.current)
    },
    [],
  )

  function showDismissed(alert: LineupAlert) {
    if (dismissTimer.current !== undefined) clearTimeout(dismissTimer.current)
    setDismissedAlert(alert)
    dismissTimer.current = setTimeout(() => {
      setDismissedAlert((current) =>
        current?.fingerprint === alert.fingerprint ? null : current,
      )
      dismissTimer.current = undefined
    }, 6000)
  }

  function dismissAlert(alert: LineupAlert) {
    showDismissed(alert)
    void dismissal.dismiss(alert).catch(() => {
      setDismissedAlert((current) =>
        current?.fingerprint === alert.fingerprint ? null : current,
      )
    })
  }

  function undoDismissal(alert: LineupAlert) {
    if (dismissTimer.current !== undefined) clearTimeout(dismissTimer.current)
    dismissTimer.current = undefined
    setDismissedAlert(null)
    void dismissal.undo(alert).catch(() => showDismissed(alert))
  }

  return (
    <Section
      title="Needs attention"
      count={query.data ? alerts.length : undefined}
    >
      {query.isPending ? (
        <SectionLoading />
      ) : query.isError ? (
        <SectionError
          retry={() => {
            void query.refetch()
          }}
        />
      ) : (
        <>
          {dismissedAlert && (
            <DismissedNotice alert={dismissedAlert} onUndo={undoDismissal} />
          )}
          {alerts.length ? (
            <div className="grid gap-3">
              {shown.map((alert) => (
                <AlertCard
                  key={alert.id}
                  alert={alert}
                  onDismiss={dismissAlert}
                />
              ))}
            </div>
          ) : (
            <SectionMessage>
              {query.data.coverage?.state === "empty"
                ? "Import a league to check your lineup."
                : "Nothing needs attention in your latest synced lineups."}
            </SectionMessage>
          )}
          {alerts.length > 3 && (
            <Button
              variant="ghost"
              size="sm"
              className="mt-1"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
            >
              {expanded
                ? "Show less"
                : `Show ${hidden.length} more${urgent ? ` · ${urgent} need action` : ""}`}
            </Button>
          )}
        </>
      )}
    </Section>
  )
}
