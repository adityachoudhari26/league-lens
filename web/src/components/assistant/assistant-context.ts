import { create } from "@bufbuild/protobuf"
import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"
import { useRouterState } from "@tanstack/react-router"
import {
  PageContextSchema,
  type PageContext,
} from "@/gen/leaguelens/v1/assistant_pb"
import { matchupQueries } from "@/lib/api/matchups"

export function useAssistantPageContext(): PageContext {
  const location = useRouterState({ select: (state) => state.location })
  const pathname = location.pathname
  const search = location.search as { league?: unknown; week?: unknown }
  const matchups = pathname === "/matchups"
  const leagues = useQuery({ ...matchupQueries.leagues, enabled: matchups })
  const eligibleLeagues =
    leagues.data?.leagues.filter(
      (league) => league.selectedTeamIds.length > 0,
    ) ?? []
  const requestedLeague =
    typeof search.league === "string" ? search.league : undefined
  const selectedLeague =
    eligibleLeagues.find((league) => league.id === requestedLeague) ??
    eligibleLeagues[0]
  const leagueSeasonId = matchups
    ? (selectedLeague?.id ?? requestedLeague ?? "")
    : ""
  const teamId = matchups ? (selectedLeague?.selectedTeamIds[0] ?? "") : ""
  const week =
    matchups && typeof search.week === "number" && Number.isInteger(search.week)
      ? search.week
      : undefined

  return useMemo(
    () =>
      create(PageContextSchema, {
        path: pathname,
        leagueSeasonId,
        teamId,
        week,
      }),
    [leagueSeasonId, pathname, teamId, week],
  )
}
