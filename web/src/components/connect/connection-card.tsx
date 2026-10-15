import {
  ConnectionStatus,
  type LeagueConnection,
} from "@/gen/leaguelens/v1/connections_pb"
import { Button } from "@/components/ui/button"
import { ConnectedLeagueRow } from "./connected-league-row"

export function ConnectionCard({
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
