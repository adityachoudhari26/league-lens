import { useCallback, useEffect, useRef } from "react"
import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { type LeagueSyncStatus } from "@/gen/leaguelens/v1/sync_pb"
import { sync } from "./client"

const syncPollIntervalMs = 2000

export function syncingLeagues(leagues: LeagueSyncStatus[] = []) {
  return leagues.filter((league) => league.state === "syncing")
}

// Polls only while a league is syncing.
export const syncStatusQuery = queryOptions({
  queryKey: ["sync", "status"],
  queryFn: ({ signal }) => sync.getSyncStatus({}, { signal }),
  refetchInterval: (query) => (syncingLeagues(query.state.data?.leagues).length ? syncPollIntervalMs : false),
})

/**
 * Keeps provider data current: opening the page syncs stale leagues in the
 * background, refresh syncs every league, and the overview reloads as each
 * league finishes. Saved data stays visible throughout.
 */
export function useLeagueSync() {
  const queryClient = useQueryClient()
  const status = useQuery(syncStatusQuery)
  const trackSyncingLeagues = useOverviewReloadOnSyncFinish(status.data?.leagues)
  const start = useMutation({
    mutationFn: (onlyStale: boolean) => sync.startSync({ onlyStale }),
    onSuccess: (response) => {
      trackSyncingLeagues(response.leagues)
      return queryClient.invalidateQueries({ queryKey: syncStatusQuery.queryKey })
    },
  })
  useStaleSyncOnMount(start.mutate)

  const leagues = status.data?.leagues ?? []
  return {
    leagues,
    syncing: start.isPending || syncingLeagues(leagues).length > 0,
    syncUnavailable: start.isError || status.isError,
    refresh: () => { start.mutate(false) },
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

// Reloads overview sections once any tracked league leaves the syncing state.
function useOverviewReloadOnSyncFinish(leagues?: LeagueSyncStatus[]) {
  const queryClient = useQueryClient()
  const syncingIds = useRef(new Set<string>())

  useEffect(() => {
    const stillSyncing = new Set(syncingLeagues(leagues).map((league) => league.leagueSeasonId))
    const finished = [...syncingIds.current].some((id) => !stillSyncing.has(id))
    syncingIds.current = stillSyncing
    if (finished) void queryClient.invalidateQueries({ queryKey: ["overview"] })
  }, [leagues, queryClient])

  return useCallback((started: LeagueSyncStatus[]) => {
    for (const league of syncingLeagues(started)) syncingIds.current.add(league.leagueSeasonId)
  }, [])
}
