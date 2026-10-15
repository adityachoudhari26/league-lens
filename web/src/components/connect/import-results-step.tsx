import { Check, CircleAlert } from "lucide-react"
import type {
  DiscoveredLeague,
  ImportResult,
} from "@/gen/leaguelens/v1/connections_pb"
import { Button } from "@/components/ui/button"
import { importErrorMessage } from "@/lib/api/connections"

export function ImportResultsStep({
  hidden,
  results,
  leagues,
  onDone,
}: {
  hidden: boolean
  results: ImportResult[]
  leagues: DiscoveredLeague[]
  onDone: () => void
}) {
  return (
    <div hidden={hidden} className={hidden ? "hidden" : "grid gap-4"}>
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
              result={result}
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
  result,
  name,
  season,
}: {
  result: ImportResult
  name: string
  season?: number
}) {
  return (
    <div className="flex items-start gap-3 rounded-lg border px-3 py-3">
      {result.succeeded ? (
        <Check className="mt-0.5 size-4 shrink-0 text-emerald-700 dark:text-[#9bc8bf]" />
      ) : (
        <CircleAlert className="mt-0.5 size-4 shrink-0 text-amber-700 dark:text-[#d8b77b]" />
      )}
      <div className="min-w-0">
        <p className="text-sm font-medium">
          {name}
          {season ? ` · ${season}` : ""}
        </p>
        {result.succeeded ? (
          <p className="mt-1 text-xs text-muted-foreground">Imported</p>
        ) : (
          <p
            role="alert"
            className="mt-1 text-sm text-amber-800 dark:text-[#e0c48f]"
          >
            {importErrorMessage(result.errorCode)}
          </p>
        )}
      </div>
    </div>
  )
}
