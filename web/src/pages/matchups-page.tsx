import { useEffect } from "react"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { type NavigationItem } from "@/components/layout/navigation"
import {
  LeagueSelector,
  WeekSelector,
} from "@/components/matchups/matchup-controls"
import { MatchupCard } from "@/components/matchups/matchup-card"
import { SectionMessage } from "@/components/overview/section"
import { Button } from "@/components/ui/button"
import { type League } from "@/gen/leaguelens/v1/leagues_pb"
import { matchupQueries } from "@/lib/api/matchups"
import { useReloadAfterSync } from "@/lib/api/sync"
import { PageHeading } from "@/pages/workspace-page"

function LoadError({ retry }: { retry: () => void }) {
  return (
    <SectionMessage>
      <p role="alert">Couldn’t load matchups.</p>
      <Button variant="outline" size="sm" className="mt-3" onClick={retry}>
        Try again
      </Button>
    </SectionMessage>
  )
}

function MatchupContent({
  query,
}: {
  query: ReturnType<typeof useMatchupDetail>
}) {
  if (query.isPending)
    return (
      <SectionMessage>
        <span role="status">Loading…</span>
      </SectionMessage>
    )
  if (query.isError)
    return (
      <LoadError
        retry={() => {
          void query.refetch()
        }}
      />
    )
  const matchup = query.data.matchup
  if (!matchup?.team)
    return <SectionMessage>No matchup this week.</SectionMessage>
  return <MatchupCard matchup={matchup} team={matchup.team} />
}

// Keeps the previous week on screen while the next one loads.
function useMatchupDetail(leagueSeasonId: string, period?: number) {
  return useQuery({
    ...matchupQueries.detail(leagueSeasonId, period),
    placeholderData: keepPreviousData,
  })
}

/**
 * One league's matchup. With no week chosen, the server returns the current
 * round, which then shows as the selected week.
 */
function LeagueMatchup({
  leagues,
  league,
  onSelectLeague,
}: {
  leagues: League[]
  league: League
  onSelectLeague: (leagueId: string) => void
}) {
  const { week } = useSearch({ from: "/matchups" })
  const navigate = useNavigate({ from: "/matchups" })
  const periods = useQuery(matchupQueries.periods(league.id))
  const detail = useMatchupDetail(league.id, week)
  const shownPeriod = week ?? detail.data?.matchup?.matchupPeriod
  const onSelectWeek = (period: number) => {
    void navigate({
      search: { league: league.id, week: period },
      replace: true,
    })
  }
  return (
    <div className="mx-auto grid w-full max-w-4xl gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <LeagueSelector
          leagues={leagues}
          selected={league.id}
          onSelect={onSelectLeague}
        />
        {periods.data?.length ? (
          <WeekSelector
            periods={periods.data}
            period={shownPeriod}
            onSelect={onSelectWeek}
          />
        ) : null}
      </div>
      <MatchupContent query={detail} />
    </div>
  )
}

// Leagues where the owner has a selected team; the first is the default.
function MatchupsView({ leagues }: { leagues: League[] }) {
  const { league: leagueId } = useSearch({ from: "/matchups" })
  const navigate = useNavigate({ from: "/matchups" })
  const league =
    leagues.find((current) => current.id === leagueId) ?? leagues[0]
  const onSelectLeague = (id: string) => {
    void navigate({ search: { league: id }, replace: true })
  }
  return (
    <LeagueMatchup
      key={league.id}
      leagues={leagues}
      league={league}
      onSelectLeague={onSelectLeague}
    />
  )
}

function MatchupsBody() {
  const query = useQuery(matchupQueries.leagues)
  if (query.isPending)
    return (
      <SectionMessage>
        <span role="status">Loading…</span>
      </SectionMessage>
    )
  if (query.isError)
    return (
      <LoadError
        retry={() => {
          void query.refetch()
        }}
      />
    )
  const leagues = query.data.leagues.filter(
    (league) => league.selectedTeamIds.length > 0,
  )
  if (!leagues.length)
    return <SectionMessage>No leagues with a selected team yet.</SectionMessage>
  return <MatchupsView leagues={leagues} />
}

export function MatchupsPage({ item }: { item: NavigationItem }) {
  useReloadAfterSync()
  useEffect(() => {
    document.title = `${item.title} · League Lens`
  }, [item.title])
  return (
    <>
      <PageHeading item={item} />
      <MatchupsBody />
    </>
  )
}
