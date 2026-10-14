import { timestampDate } from "@bufbuild/protobuf/wkt"
import { type LeagueSyncStatus } from "@/gen/leaguelens/v1/sync_pb"
import { syncingLeagues } from "@/lib/api/sync"
import { observedTime } from "./format"

const failureReasons: Record<string, string> = {
  espn_credentials_missing: "ESPN cookies aren’t configured on the server",
  espn_account_mismatch:
    "the server’s ESPN cookies belong to a different account",
  provider_unavailable: "the provider couldn’t be reached",
  provider_fetch_failed: "the provider couldn’t be reached",
  invalid_provider_data: "the provider returned data we couldn’t use",
  import_write_failed: "the update couldn’t be saved",
  sync_timed_out: "the provider took too long to respond",
}

function failureReason(code: string) {
  return failureReasons[code] ?? "something went wrong"
}

function oldestSuccessfulSync(leagues: LeagueSyncStatus[]) {
  return leagues
    .flatMap((league) =>
      league.lastSuccessfulSyncAt ? [league.lastSuccessfulSyncAt] : [],
    )
    .sort((a, b) => timestampDate(a).getTime() - timestampDate(b).getTime())[0]
}

function SyncFailure({ league }: { league: LeagueSyncStatus }) {
  const savedData = league.lastSuccessfulSyncAt
    ? `Showing data from ${observedTime(league.lastSuccessfulSyncAt)}.`
    : "No saved data yet."
  return (
    <p
      role="alert"
      className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-amber-900 dark:border-[#5a4a2e] dark:bg-[#332b1f] dark:text-[#e0c48f]"
    >
      <span className="font-medium">
        {league.provider.toUpperCase()} · {league.leagueName}:
      </span>{" "}
      sync failed because {failureReason(league.errorCode)}. {savedData}
    </p>
  )
}

/** Explains whether provider data is syncing, current, or stale after a failure. */
export function SyncStatus({
  leagues,
  syncing,
  syncUnavailable,
}: {
  leagues: LeagueSyncStatus[]
  syncing: boolean
  syncUnavailable: boolean
}) {
  const active = syncingLeagues(leagues)
  const failed = leagues.filter((league) => league.state === "failed")
  const lastSynced = oldestSuccessfulSync(leagues)
  const showLastSynced =
    !syncing && !failed.length && !syncUnavailable && lastSynced
  if (!syncUnavailable && !active.length && !failed.length && !showLastSynced)
    return null

  return (
    <div className="mb-4 grid gap-2 text-sm">
      {syncUnavailable && (
        <p role="alert" className="text-muted-foreground">
          Couldn’t reach the sync service. Showing saved data.
        </p>
      )}
      {active.length > 0 && (
        <p role="status" className="text-muted-foreground">
          Syncing {active.map((league) => league.leagueName).join(", ")}…
        </p>
      )}
      {failed.map((league) => (
        <SyncFailure key={league.leagueSeasonId} league={league} />
      ))}
      {showLastSynced && (
        <p className="text-xs text-muted-foreground">
          All leagues synced · oldest update {observedTime(lastSynced)}
        </p>
      )}
    </div>
  )
}
