import { useEffect, useRef } from "react"
import { create } from "@bufbuild/protobuf"
import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import {
  GetSyncStatusResponseSchema,
  type LeagueSyncStatus,
} from "@/gen/leaguelens/v1/sync_pb"
import { sync } from "./client"

const syncPollIntervalMs = 2000

export function syncingLeagues(leagues: LeagueSyncStatus[] = []) {
  return leagues.filter((league) => league.state === "syncing")
}

// Polls only while a league is syncing.
export const syncStatusQuery = queryOptions({
  queryKey: ["sync", "status"],
  queryFn: ({ signal }) => sync.getSyncStatus({}, { signal }),
  refetchInterval: (query) =>
    syncingLeagues(query.state.data?.leagues).length
      ? syncPollIntervalMs
      : false,
})

/**
 * Keeps provider data current: opening the page syncs stale leagues in the
 * background, refresh syncs every league, and the overview reloads when
 * syncing ends. Saved data stays visible throughout.
 */
export function useLeagueSync() {
  const queryClient = useQueryClient()
  const status = useQuery(syncStatusQuery)
  const start = useMutation({
    mutationFn: (onlyStale: boolean) => sync.startSync({ onlyStale }),
    // Cancel any in-flight status read first so a pre-claim response can't
    // overwrite the syncing state and skip the overview reload.
    onSuccess: async (response) => {
      await queryClient.cancelQueries({ queryKey: syncStatusQuery.queryKey })
      queryClient.setQueryData(
        syncStatusQuery.queryKey,
        create(GetSyncStatusResponseSchema, { leagues: response.leagues }),
      )
    },
  })
  useStaleSyncOnMount(start.mutate)

  const leagues = status.data?.leagues ?? []
  const leaguesSyncing = syncingLeagues(leagues).length > 0
  useReloadWhenSyncEnds(leaguesSyncing)
  return {
    leagues,
    syncing: start.isPending || leaguesSyncing,
    syncUnavailable: start.isError || status.isError,
    refresh: () => {
      start.mutate(false)
    },
  }
}

function useStaleSyncOnMount(startSync: (onlyStale: boolean) => void) {
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
    startSync(true)
  }, [startSync])
}

/** Reloads saved data when a sync finishes, without starting one. */
export function useReloadAfterSync() {
  const status = useQuery(syncStatusQuery)
  useReloadWhenSyncEnds(syncingLeagues(status.data?.leagues).length > 0)
}

// Reloads every saved-data query (all but sync status) when the last syncing
// league finishes. Inactive pages are only marked stale.
function useReloadWhenSyncEnds(syncing: boolean) {
  const queryClient = useQueryClient()
  const wasSyncing = useRef(false)

  useEffect(() => {
    if (wasSyncing.current && !syncing)
      void queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] !== "sync",
      })
    wasSyncing.current = syncing
  }, [syncing, queryClient])
}
