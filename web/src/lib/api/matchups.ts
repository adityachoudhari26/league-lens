import { queryOptions } from "@tanstack/react-query"
import { dashboard } from "./client"

// Reads saved snapshots. Matchups reloads these queries after syncs and polls
// live matchup details; it never starts a sync itself.
export const matchupQueries = {
  leagues: queryOptions({
    queryKey: ["matchups", "leagues"],
    queryFn: ({ signal }) =>
      dashboard.listLeagues({ pageSize: 100 }, { signal }),
  }),
  periods: (leagueSeasonId: string) =>
    queryOptions({
      queryKey: ["matchups", "periods", leagueSeasonId],
      queryFn: ({ signal }) =>
        dashboard.getLeague({ leagueSeasonId }, { signal }),
      select: (response) => response.availableMatchupPeriods,
    }),
  // An omitted period asks the server for the current round.
  detail: (leagueSeasonId: string, matchupPeriod?: number) =>
    queryOptions({
      queryKey: [
        "matchups",
        "detail",
        leagueSeasonId,
        matchupPeriod ?? "current",
      ],
      queryFn: ({ signal }) =>
        dashboard.getMatchupDetail(
          { leagueSeasonId, matchupPeriod },
          { signal },
        ),
    }),
  // The graph's own query: samples and key moments for one matchup.
  // `building` means a finished week is being rebuilt; ask again shortly.
  winChance: (matchupId: string) =>
    queryOptions({
      queryKey: ["matchups", "win-chance", matchupId],
      queryFn: ({ signal }) =>
        dashboard.getMatchupWinChance({ matchupId }, { signal }),
    }),
  // Who has the ball in each NFL game in progress, from the public scoreboard.
  liveGames: queryOptions({
    queryKey: ["matchups", "live-games"],
    queryFn: ({ signal }) => dashboard.getLiveGames({}, { signal }),
    select: (response) => response.games,
  }),
}
