import { useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { ConnectDialog } from "@/components/connect/connect-dialog"
import { AttentionSection } from "@/components/overview/attention-section"
import { MatchupsSection } from "@/components/overview/matchups-section"
import { UpdatesSection } from "@/components/overview/updates-section"
import { LookingAhead } from "@/components/overview/looking-ahead-section"
import { SyncStatus } from "@/components/overview/sync-status"
import { ConnectionStatus } from "@/gen/leaguelens/v1/connections_pb"
import { connectionQueries } from "@/lib/api/connections"
import { useLeagueSync } from "@/lib/api/sync"

export function OverviewPage() {
  const { leagues, syncing, syncUnavailable, refresh } = useLeagueSync()
  const connectionQuery = useQuery(connectionQueries.list)
  const [connectOpen, setConnectOpen] = useState(false)
  const [initialProvider, setInitialProvider] = useState<"espn" | undefined>()
  useEffect(() => {
    document.title = "Overview · League Lens"
  }, [])
  const connections = connectionQuery.data?.connections ?? []
  const hasAttachedLeagues = connections.some(
    (connection) => connection.leagues.length > 0,
  )
  const showEmptyState = connectionQuery.isSuccess && !hasAttachedLeagues
  const needsReconnect = connections.some(
    (connection) =>
      connection.provider === "espn" &&
      connection.status === ConnectionStatus.NEEDS_RECONNECT,
  )

  function openConnect(provider?: "espn") {
    setInitialProvider(provider)
    setConnectOpen(true)
  }

  function handleDialogOpenChange(open: boolean) {
    setConnectOpen(open)
    if (!open) setInitialProvider(undefined)
  }

  return (
    <>
      <div>
        <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">
              Your week at a glance
            </h1>
            <p className="mt-1 text-sm text-muted-foreground">
              A daily view across your leagues.
            </p>
          </div>
          <div className="flex items-center gap-2">
            {showEmptyState ? (
              <Button onClick={() => openConnect()} size="sm">
                Connect leagues
              </Button>
            ) : (
              <>
                <span className="rounded-md border border-teal-200 bg-teal-50 px-2.5 py-1.5 text-xs font-medium text-teal-800 dark:border-[#3b514d] dark:bg-[#283734] dark:text-[#9bc8bf]">
                  All leagues
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={syncing}
                  onClick={refresh}
                >
                  {syncing ? "Syncing…" : "Refresh"}
                </Button>
              </>
            )}
          </div>
        </div>
        {needsReconnect && (
          <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-[#5a4a2e] dark:bg-[#332b1f] dark:text-[#e0c48f]">
            <p role="alert">
              ESPN rejected the saved cookies, so ESPN leagues can't sync.
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => openConnect("espn")}
            >
              Reconnect
            </Button>
          </div>
        )}
        <SyncStatus
          leagues={leagues}
          syncing={syncing}
          syncUnavailable={syncUnavailable}
        />
        {showEmptyState ? (
          <section className="rounded-xl border bg-card px-5 py-8 text-center sm:px-8">
            <h2 className="text-lg font-semibold">Connect your first league</h2>
            <p className="mt-2 text-sm text-muted-foreground">
              Connect ESPN or Sleeper to bring your league into League Lens.
            </p>
            <Button className="mt-5" onClick={() => openConnect()}>
              Connect leagues
            </Button>
          </section>
        ) : (
          <div className="grid items-start gap-x-8 gap-y-10 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]">
            <div className="grid gap-10">
              <AttentionSection />
              <UpdatesSection />
              <LookingAhead />
            </div>
            <MatchupsSection />
          </div>
        )}
      </div>
      {connectOpen && (
        <ConnectDialog
          open={connectOpen}
          onOpenChange={handleDialogOpenChange}
          initialProvider={initialProvider}
          showSettingsHint
        />
      )}
    </>
  )
}
