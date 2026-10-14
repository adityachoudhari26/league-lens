import { create } from "@bufbuild/protobuf"
import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"
import {
  GetOverviewAttentionResponseSchema,
  type LineupAlert,
} from "@/gen/leaguelens/v1/overview_pb"
import { dashboard } from "./client"

// Reads saved snapshots. Provider syncs run through useLeagueSync, which reloads
// these queries when syncing ends.
export const overviewQueries = {
  attention: queryOptions({
    queryKey: ["overview", "attention"],
    queryFn: ({ signal }) => dashboard.getOverviewAttention({}, { signal }),
  }),
  matchups: queryOptions({
    queryKey: ["overview", "matchups"],
    queryFn: ({ signal }) => dashboard.getOverviewMatchups({}, { signal }),
  }),
  updates: queryOptions({
    queryKey: ["overview", "updates"],
    queryFn: ({ signal }) => dashboard.getOverviewPlayerUpdates({}, { signal }),
  }),
  ahead: queryOptions({
    queryKey: ["overview", "ahead"],
    queryFn: ({ signal }) => dashboard.getOverviewLookingAhead({}, { signal }),
  }),
}

export function useDismissLineupAlert() {
  const queryClient = useQueryClient()
  const attentionKey = overviewQueries.attention.queryKey
  const dismiss = useMutation({
    mutationFn: (alert: LineupAlert) =>
      dashboard.dismissLineupAlert({ fingerprint: alert.fingerprint }),
    onMutate: async (alert) => {
      await queryClient.cancelQueries({ queryKey: attentionKey })
      const previous = queryClient.getQueryData(attentionKey)
      if (previous) {
        queryClient.setQueryData(
          attentionKey,
          create(GetOverviewAttentionResponseSchema, {
            ...previous,
            alerts: previous.alerts.filter(
              (current) => current.fingerprint !== alert.fingerprint,
            ),
          }),
        )
      }
      return { previous }
    },
    onError: (_error, _alert, context) => {
      if (context?.previous)
        queryClient.setQueryData(attentionKey, context.previous)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: attentionKey }),
  })
  const restore = useMutation({
    mutationFn: (alert: LineupAlert) =>
      dashboard.restoreLineupAlert({ fingerprint: alert.fingerprint }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: attentionKey }),
  })

  return {
    dismiss: (alert: LineupAlert) => dismiss.mutateAsync(alert),
    undo: (alert: LineupAlert) => restore.mutateAsync(alert),
  }
}
