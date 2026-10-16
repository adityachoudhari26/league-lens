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
const staleCheckIntervalMs = 5 * 60 * 1000
const staleCheckMinGapMs = 60 * 1000

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

export function useStartSync() {
  const queryClient = useQueryClient()
  return useMutation({
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
}

/**
 * Checks on page open, tab return, and every few minutes while visible. The
 * server skips fresh leagues; the min gap avoids duplicate requests from
 * focus and visibilitychange events or StrictMode's double mount.
 */
export function useStaleSyncChecks() {
  const { mutate: startSync } = useStartSync()
  const lastCheckAt = useRef(0)
  useEffect(() => {
    const checkIfVisible = () => {
      if (document.visibilityState !== "visible") return
      const now = Date.now()
      if (now - lastCheckAt.current < staleCheckMinGapMs) return
      lastCheckAt.current = now
      startSync(true)
    }
    checkIfVisible()
    document.addEventListener("visibilitychange", checkIfVisible)
    window.addEventListener("focus", checkIfVisible)
    const interval = window.setInterval(checkIfVisible, staleCheckIntervalMs)
    return () => {
      document.removeEventListener("visibilitychange", checkIfVisible)
      window.removeEventListener("focus", checkIfVisible)
      window.clearInterval(interval)
    }
  }, [startSync])
}

/**
 * Keeps provider data current with background stale checks and explicit
 * refreshes. The overview reloads when syncing ends; saved data stays visible.
 */
export function useLeagueSync() {
  const status = useQuery(syncStatusQuery)
  const refreshSync = useStartSync()
  useStaleSyncChecks()

  const leagues = status.data?.leagues ?? []
  const leaguesSyncing = syncingLeagues(leagues).length > 0
  useReloadWhenSyncEnds(leaguesSyncing)
  return {
    leagues,
    syncing: refreshSync.isPending || leaguesSyncing,
    syncUnavailable: refreshSync.isError || status.isError,
    refresh: () => refreshSync.mutate(false),
  }
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
