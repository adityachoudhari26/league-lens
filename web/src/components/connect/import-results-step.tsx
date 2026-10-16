import { useQuery } from "@tanstack/react-query"
import { Check, CircleAlert, Loader2 } from "lucide-react"
import type {
  DiscoveredLeague,
  ImportResult,
} from "@/gen/leaguelens/v1/connections_pb"
import type { LeagueSyncStatus } from "@/gen/leaguelens/v1/sync_pb"
import { Button } from "@/components/ui/button"
import { importErrorMessage } from "@/lib/api/connections"
import { syncStatusQuery, useReloadAfterSync } from "@/lib/api/sync"

// The live state a result row shows: still running, finished, or failed with a code.
type ImportRowState = "importing" | "imported" | { failed: string }

// A pick that never started fails right away; a started pick follows its
// league's sync status until it settles.
function importRowState(
  result: ImportResult,
  statuses: LeagueSyncStatus[],
): ImportRowState {
  if (!result.started) return { failed: result.errorCode }
  const status = statuses.find(
    (league) => league.leagueSeasonId === result.leagueId,
  )
  if (status?.state === "succeeded") return "imported"
  if (status?.state === "failed") return { failed: status.errorCode }
  return "importing"
}

export function ImportResultsStep({
  results,
  leagues,
  onDone,
}: {
  results: ImportResult[]
  leagues: DiscoveredLeague[]
  onDone: () => void
}) {
  const status = useQuery(syncStatusQuery)
  useReloadAfterSync()
  const statuses = status.data?.leagues ?? []
  return (
    <div className="grid gap-4">
      <div className="grid gap-2">
        {results.map((result) => {
          const league = leagues.find(
            (candidate) =>
              candidate.externalLeagueId === result.externalLeagueId &&
              candidate.teamId === result.teamId,
          )
          return (
            <ImportResultRow
              key={`${result.externalLeagueId}:${result.teamId}`}
              state={importRowState(result, statuses)}
              name={league?.name ?? result.externalLeagueId}
              season={league?.season}
            />
          )
        })}
      </div>
      <Button type="button" onClick={onDone}>
        Done
      </Button>
    </div>
  )
}

function ImportResultRow({
  state,
  name,
  season,
}: {
  state: ImportRowState
  name: string
  season?: number
}) {
  return (
    <div className="flex items-start gap-3 rounded-lg border px-3 py-3">
      <RowIcon state={state} />
      <div className="min-w-0">
        <p className="text-sm font-medium">
          {name}
          {season ? ` · ${season}` : ""}
        </p>
        <RowMessage state={state} />
      </div>
    </div>
  )
}

function RowIcon({ state }: { state: ImportRowState }) {
  if (state === "importing")
    return (
      <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />
    )
  if (state === "imported")
    return (
      <Check className="mt-0.5 size-4 shrink-0 text-emerald-700 dark:text-[#9bc8bf]" />
    )
  return (
    <CircleAlert className="mt-0.5 size-4 shrink-0 text-amber-700 dark:text-[#d8b77b]" />
  )
}

function RowMessage({ state }: { state: ImportRowState }) {
  if (state === "importing")
    return <p className="mt-1 text-xs text-muted-foreground">Importing…</p>
  if (state === "imported")
    return <p className="mt-1 text-xs text-muted-foreground">Imported</p>
  return (
    <p role="alert" className="mt-1 text-sm text-amber-800 dark:text-[#e0c48f]">
      {importErrorMessage(state.failed)}
    </p>
  )
}
