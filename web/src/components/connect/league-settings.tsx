import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ConnectionStatus,
  type LeagueConnection,
  type ConnectedLeague,
} from "@/gen/leaguelens/v1/connections_pb"
import { connectionQueries, useRemoveLeague } from "@/lib/api/connections"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { observedTime } from "@/components/overview/format"
import { ConnectDialog } from "./connect-dialog"

type Provider = "espn" | "sleeper"

export function LeagueSettings() {
  const query = useQuery(connectionQueries.list)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [initialProvider, setInitialProvider] = useState<Provider>()
  const connections = query.data?.connections ?? []

  function openConnect(provider?: Provider) {
    setInitialProvider(provider)
    setDialogOpen(true)
  }

  function handleDialogOpenChange(open: boolean) {
    setDialogOpen(open)
    if (!open) setInitialProvider(undefined)
  }

  return (
    <>
      <section
        className="rounded-xl border bg-card p-6 sm:p-8"
        aria-labelledby="leagues-heading"
      >
        <header className="mb-6 flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 id="leagues-heading" className="text-base font-medium">
              Leagues
            </h2>
            <p className="mt-1.5 text-sm text-muted-foreground">
              Connect fantasy accounts and manage the leagues shown in League
              Lens.
            </p>
          </div>
          <Button onClick={() => openConnect()}>Connect leagues</Button>
        </header>

        {query.isPending ? (
          <div
            className="grid gap-3"
            role="status"
            aria-label="Loading leagues"
          >
            <Skeleton className="h-24 w-full rounded-lg" />
            <Skeleton className="h-24 w-full rounded-lg" />
          </div>
        ) : query.isError ? (
          <div className="rounded-lg border px-4 py-3">
            <p role="alert" className="text-sm text-muted-foreground">
              Couldn’t load connections. Check that the API is running and try
              again.
            </p>
            <Button
              variant="outline"
              size="sm"
              className="mt-3"
              onClick={() => void query.refetch()}
            >
              Try again
            </Button>
          </div>
        ) : connections.length === 0 ? (
          <p className="rounded-lg border px-4 py-3 text-sm text-muted-foreground">
            No leagues connected yet.
          </p>
        ) : (
          <div className="grid gap-3">
            {connections.map((connection) => (
              <ConnectionCard
                key={connection.id}
                connection={connection}
                onAddLeagues={() =>
                  openConnect(
                    connection.provider === "espn" ? "espn" : "sleeper",
                  )
                }
              />
            ))}
          </div>
        )}
      </section>
      {dialogOpen && (
        <ConnectDialog
          open={dialogOpen}
          onOpenChange={handleDialogOpenChange}
          initialProvider={initialProvider}
        />
      )}
    </>
  )
}

function ConnectionCard({
  connection,
  onAddLeagues,
}: {
  connection: LeagueConnection
  onAddLeagues: () => void
}) {
  const isEspn = connection.provider === "espn"
  const needsReconnect = connection.status === ConnectionStatus.NEEDS_RECONNECT
  const providerName = isEspn ? "ESPN" : "Sleeper"

  return (
    <article className="rounded-lg border px-4 py-3">
      <header className="flex flex-wrap items-center gap-2">
        <h3 className="font-medium">{providerName}</h3>
        <ConnectionStatusBadge status={connection.status} />
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          onClick={onAddLeagues}
        >
          {isEspn && needsReconnect ? "Reconnect" : "Add leagues"}
        </Button>
      </header>
      {connection.leagues.length ? (
        <ul className="mt-3 divide-y">
          {connection.leagues.map((league) => (
            <ConnectedLeagueRow key={league.leagueId} league={league} />
          ))}
        </ul>
      ) : (
        <p className="mt-3 text-sm text-muted-foreground">
          No leagues imported.
        </p>
      )}
    </article>
  )
}

function ConnectionStatusBadge({ status }: { status: ConnectionStatus }) {
  const needsReconnect = status === ConnectionStatus.NEEDS_RECONNECT
  const labels: Record<ConnectionStatus, string> = {
    [ConnectionStatus.UNSPECIFIED]: "Unknown",
    [ConnectionStatus.PENDING]: "Pending",
    [ConnectionStatus.ACTIVE]: "Active",
    [ConnectionStatus.NEEDS_RECONNECT]: "Needs reconnect",
    [ConnectionStatus.DISABLED]: "Disabled",
  }
  return (
    <span
      className={
        needsReconnect
          ? "rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-800 dark:border-[#5a4a2e] dark:bg-[#332b1f] dark:text-[#e0c48f]"
          : "rounded-full border border-border bg-muted px-2 py-0.5 text-xs text-muted-foreground"
      }
    >
      {labels[status] ?? "Unknown"}
    </span>
  )
}

function ConnectedLeagueRow({ league }: { league: ConnectedLeague }) {
  const [confirming, setConfirming] = useState(false)
  const remove = useRemoveLeague()

  async function confirmRemove() {
    try {
      await remove.mutateAsync(league.leagueId)
      setConfirming(false)
    } catch {
      return
    }
  }

  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3 first:pt-0 last:pb-0">
      <p className="min-w-0 flex-1 text-sm">
        <span className="font-medium">{league.name}</span>
        <span className="text-muted-foreground">
          {" "}
          · {league.season} · Synced {observedTime(league.lastSuccessfulSyncAt)}
        </span>
      </p>
      {confirming ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">
            Remove from League Lens? Saved history is kept.
          </span>
          <Button
            size="sm"
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => void confirmRemove()}
          >
            {remove.isPending ? "Removing…" : "Remove"}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => {
              setConfirming(false)
              remove.reset()
            }}
          >
            Cancel
          </Button>
        </div>
      ) : (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            remove.reset()
            setConfirming(true)
          }}
        >
          Remove
        </Button>
      )}
      {remove.isError && (
        <p
          role="alert"
          className="basis-full text-xs text-amber-800 dark:text-[#e0c48f]"
        >
          Couldn’t remove this league. Try again.
        </p>
      )}
    </li>
  )
}
