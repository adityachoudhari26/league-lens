import { queryOptions } from "@tanstack/react-query"
import { dashboard } from "./client"

// Reads imported snapshots only; refreshing never triggers a provider sync.
export const overviewQueries = {
  attention: queryOptions({ queryKey: ["overview", "attention"], queryFn: ({ signal }) => dashboard.getOverviewAttention({}, { signal }) }),
  matchups: queryOptions({ queryKey: ["overview", "matchups"], queryFn: ({ signal }) => dashboard.getOverviewMatchups({}, { signal }) }),
  updates: queryOptions({ queryKey: ["overview", "updates"], queryFn: ({ signal }) => dashboard.getOverviewPlayerUpdates({}, { signal }) }),
  ahead: queryOptions({ queryKey: ["overview", "ahead"], queryFn: ({ signal }) => dashboard.getOverviewLookingAhead({}, { signal }) }),
}
