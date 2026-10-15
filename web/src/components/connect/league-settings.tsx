import { useQuery } from "@tanstack/react-query"
import { connectionQueries } from "@/lib/api/connections"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ConnectDialog } from "./connect-dialog"
import { ConnectionCard } from "./connection-card"
import { useConnectDialog } from "./use-connect-dialog"
import type { Provider } from "./types"

export function LeagueSettings() {
  const query = useQuery(connectionQueries.list)
  const dialog = useConnectDialog()
  const connections = query.data?.connections ?? []

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
          <Button onClick={() => dialog.openConnect()}>Connect leagues</Button>
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
                onAddLeagues={() => {
                  const provider: Provider =
                    connection.provider === "espn" ? "espn" : "sleeper"
                  dialog.openConnect(provider)
                }}
              />
            ))}
          </div>
        )}
      </section>
      {dialog.open && (
        <ConnectDialog
          open={dialog.open}
          onOpenChange={dialog.onOpenChange}
          initialProvider={dialog.initialProvider}
        />
      )}
    </>
  )
}
