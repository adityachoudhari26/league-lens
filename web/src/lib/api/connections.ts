import { Code, ConnectError } from "@connectrpc/connect"
import {
  queryOptions,
  type QueryClient,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"
import { connections } from "./client"
import { syncStatusQuery } from "./sync"

export const connectionQueries = {
  list: queryOptions({
    queryKey: ["connections"],
    queryFn: ({ signal }) => connections.listConnections({}, { signal }),
  }),
}

export function useConnectESPN() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ s2, swid }: { s2: string; swid: string }) =>
      connections.connectESPN({ s2, swid }),
    onSuccess: () => invalidateSavedQueries(queryClient),
  })
}

export function useConnectSleeper() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (username: string) => connections.connectSleeper({ username }),
    onSuccess: () => invalidateSavedQueries(queryClient),
  })
}

export function useImportLeagues() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({
      connectionId,
      picks,
    }: {
      connectionId: string
      picks: { externalLeagueId: string; teamId: string }[]
    }) => connections.importLeagues({ connectionId, picks }),
    onSuccess: async (response) => {
      if (!response.results.some((result) => result.started)) return
      await queryClient.invalidateQueries({ queryKey: ["connections"] })
      await queryClient.fetchQuery({ ...syncStatusQuery, staleTime: 0 })
    },
  })
}

export function useRemoveLeague() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (leagueId: string) => connections.removeLeague({ leagueId }),
    onSuccess: () => invalidateSavedQueries(queryClient),
  })
}

export function connectErrorMessage(error: unknown) {
  if (!(error instanceof ConnectError))
    return "Couldn't reach League Lens. Try again."
  if (error.code === Code.PermissionDenied)
    return "ESPN rejected these cookies. Copy them again from a logged-in espn.com tab."
  if (error.code === Code.FailedPrecondition) return error.rawMessage
  if (error.code === Code.NotFound) return "No Sleeper user with that username."
  return "Couldn't reach League Lens. Try again."
}

export function importErrorMessage(code: string) {
  const messages: Record<string, string> = {
    invalid_selection: "Choose a valid team for this league.",
    needs_reconnect: "ESPN rejected the saved cookies — reconnect ESPN",
    provider_fetch_failed: "Couldn't fetch this league from the provider.",
    sync_timed_out: "Importing this league took too long. Try again.",
    sync_canceled:
      "The import stopped because League Lens shut down. Try again.",
    provider_unavailable: "Couldn't reach the provider. Try again.",
    invalid_provider_data: "The provider sent data League Lens couldn't read.",
    espn_credentials_unavailable:
      "ESPN rejected the saved cookies — reconnect ESPN",
    account_mismatch: "This league belongs to a different account.",
    import_write_failed: "Couldn't save this league. Try again.",
    import_failed: "Couldn't import this league. Try again.",
  }
  return messages[code] ?? "Couldn't import this league. Try again."
}

function invalidateSavedQueries(queryClient: QueryClient) {
  return queryClient.invalidateQueries({
    predicate: (query) => query.queryKey[0] !== "sync",
  })
}
