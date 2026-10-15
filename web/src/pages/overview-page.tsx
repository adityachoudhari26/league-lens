import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { ConnectDialog } from "@/components/connect/connect-dialog"
import { useConnectDialog } from "@/components/connect/use-connect-dialog"
import { ConnectEmptyState } from "@/components/overview/connect-empty-state"
import { EspnReconnectBanner } from "@/components/overview/espn-reconnect-banner"
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
  const connectDialog = useConnectDialog()
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
              <Button onClick={() => connectDialog.openConnect()} size="sm">
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
          <EspnReconnectBanner
            onReconnect={() => connectDialog.openConnect("espn")}
          />
        )}
        <SyncStatus
          leagues={leagues}
          syncing={syncing}
          syncUnavailable={syncUnavailable}
        />
        {showEmptyState ? (
          <ConnectEmptyState onConnect={() => connectDialog.openConnect()} />
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
      {connectDialog.open && (
        <ConnectDialog
          open={connectDialog.open}
          onOpenChange={connectDialog.onOpenChange}
          initialProvider={connectDialog.initialProvider}
          showSettingsHint
        />
      )}
    </>
  )
}
