import { queryOptions } from "@tanstack/react-query"
import { dashboard } from "./client"

// Each fetch contacts the profile source, so a card reopened within the
// session reuses the result instead of asking again.
export const playerCardQuery = (playerId: string) =>
  queryOptions({
    queryKey: ["playerCard", playerId],
    queryFn: ({ signal }) => dashboard.getPlayerCard({ playerId }, { signal }),
    select: (response) => response.card,
    staleTime: Infinity,
  })
