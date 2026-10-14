import { type ReactNode, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Activity, ArrowLeftRight, ChevronDown, Newspaper } from "lucide-react"
import {
  InjuryStatus,
  NewsCategory,
  type PlayerUpdate,
} from "@/gen/leaguelens/v1/overview_pb"
import { overviewQueries } from "@/lib/api/overview"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import {
  Section,
  SectionLoading,
  SectionError,
  SectionMessage,
  CoverageNote,
} from "./section"
import { observedTime } from "./format"

function categoryPresentation(category: NewsCategory) {
  switch (category) {
    case NewsCategory.INJURY:
      return {
        label: "Injury",
        Icon: Activity,
        accent: "text-amber-700 dark:text-[#d8b77b]",
      }
    case NewsCategory.TRANSACTION:
      return {
        label: "Transaction",
        Icon: ArrowLeftRight,
        accent: "text-sky-700 dark:text-[#9db8cc]",
      }
    default:
      return { label: "News", Icon: Newspaper, accent: "text-muted-foreground" }
  }
}

function designatedStatus(status: InjuryStatus) {
  switch (status) {
    case InjuryStatus.QUESTIONABLE:
      return "Questionable"
    case InjuryStatus.DOUBTFUL:
      return "Doubtful"
    case InjuryStatus.OUT:
      return "Out"
    case InjuryStatus.INJURED_RESERVE:
      return "Injured reserve"
    default:
      return undefined
  }
}

const previewCount = 3

/** Injured players: a designation or injury-category news. */
function isInjuryUpdate(update: PlayerUpdate) {
  return (
    update.category === NewsCategory.INJURY ||
    designatedStatus(update.injuryStatus) !== undefined
  )
}

/** Key news is about an injured player or one of the owner's starters. */
function isKeyUpdate(update: PlayerUpdate) {
  return update.starter || isInjuryUpdate(update)
}

function newsLeagues(updates: PlayerUpdate[]) {
  const leagues = new Map<string, string>()
  for (const update of updates) {
    for (const team of update.teams) leagues.set(team.leagueId, team.leagueName)
  }
  return [...leagues].map(([id, name]) => ({ id, name }))
}

type NewsFilters = { leagueId?: string; player: string; injuriesOnly: boolean }

function filterUpdates(updates: PlayerUpdate[], filters: NewsFilters) {
  const player = filters.player.trim().toLowerCase()
  return updates.filter(
    (update) =>
      (!filters.leagueId ||
        update.teams.some((team) => team.leagueId === filters.leagueId)) &&
      (!player || update.playerName.toLowerCase().includes(player)) &&
      (!filters.injuriesOnly || isInjuryUpdate(update)),
  )
}

function PlayerUpdateCard({ update }: { update: PlayerUpdate }) {
  const category = categoryPresentation(update.category)
  const status = designatedStatus(update.injuryStatus)
  const teams = update.teams.map((team) => team.leagueName).join(", ")

  return (
    <details className="group rounded-lg border border-border bg-card">
      <summary className="flex cursor-pointer list-none items-start gap-3 rounded-lg px-3 py-2.5 focus-visible:outline-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
        <category.Icon
          aria-hidden="true"
          className={`mt-0.5 size-4 shrink-0 ${category.accent}`}
        />
        <div className="min-w-0 flex-1">
          <p className="mb-1 text-xs text-muted-foreground">
            <span className={`font-medium ${category.accent}`}>
              {category.label}
            </span>
            {status && <> · {status}</>}
            {teams && <> · {teams}</>}
          </p>
          <p className="text-sm font-semibold text-foreground/80">
            {update.playerName}
          </p>
          <h3 className="mt-0.5 text-sm font-medium">{update.headline}</h3>
          {update.summary && (
            <p className="mt-1 line-clamp-2 text-sm text-muted-foreground group-open:hidden">
              {update.summary}
            </p>
          )}
        </div>
        <ChevronDown
          aria-hidden="true"
          className="size-4 shrink-0 text-muted-foreground group-open:rotate-180"
        />
      </summary>
      <div className="border-t px-3 py-2.5 text-sm leading-6 text-muted-foreground">
        {update.summary && <p>{update.summary}</p>}
        <p className="mt-2 text-xs">
          {update.sourceLabel} · {observedTime(update.publishedAt)}
        </p>
        <a
          className="mt-2 inline-block text-sm font-medium text-primary underline-offset-4 hover:underline"
          href={update.sourceUrl}
          target="_blank"
          rel="noreferrer"
        >
          Read on ESPN
        </a>
      </div>
    </details>
  )
}

function PlayerUpdateList({ updates }: { updates: PlayerUpdate[] }) {
  return (
    <div className="grid gap-3">
      {updates.map((update) => (
        <PlayerUpdateCard key={update.id} update={update} />
      ))}
    </div>
  )
}

function FilterChip({
  pressed,
  onClick,
  children,
}: {
  pressed: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Button
      variant={pressed ? "default" : "outline"}
      size="sm"
      aria-pressed={pressed}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

function NewsFilterBar({
  leagues,
  filters,
  onChange,
}: {
  leagues: { id: string; name: string }[]
  filters: NewsFilters
  onChange: (filters: NewsFilters) => void
}) {
  return (
    <div className="grid gap-2">
      <Input
        type="search"
        placeholder="Search players"
        aria-label="Search players"
        value={filters.player}
        onChange={(event) =>
          onChange({ ...filters, player: event.target.value })
        }
      />
      <div className="flex flex-wrap gap-1.5">
        <FilterChip
          pressed={!filters.leagueId}
          onClick={() => onChange({ ...filters, leagueId: undefined })}
        >
          All leagues
        </FilterChip>
        {leagues.map((league) => (
          <FilterChip
            key={league.id}
            pressed={filters.leagueId === league.id}
            onClick={() => onChange({ ...filters, leagueId: league.id })}
          >
            {league.name}
          </FilterChip>
        ))}
        <FilterChip
          pressed={filters.injuriesOnly}
          onClick={() =>
            onChange({ ...filters, injuriesOnly: !filters.injuriesOnly })
          }
        >
          Injuries only
        </FilterChip>
      </div>
    </div>
  )
}

function PlayerNewsSheet({ updates }: { updates: PlayerUpdate[] }) {
  const [filters, setFilters] = useState<NewsFilters>({
    player: "",
    injuriesOnly: false,
  })
  const leagues = useMemo(() => newsLeagues(updates), [updates])
  const filtered = filterUpdates(updates, filters)

  return (
    <Sheet>
      <SheetTrigger render={<Button variant="ghost" size="sm" />}>
        View all {updates.length}
      </SheetTrigger>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-lg">
        <SheetHeader className="gap-3 border-b">
          <div>
            <SheetTitle>Player updates</SheetTitle>
            <SheetDescription>
              {filtered.length} of {updates.length} players, ranked injuries and
              starters first.
            </SheetDescription>
          </div>
          <NewsFilterBar
            leagues={leagues}
            filters={filters}
            onChange={setFilters}
          />
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          {filtered.length ? (
            <PlayerUpdateList updates={filtered} />
          ) : (
            <SectionMessage>No news matches these filters.</SectionMessage>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function newsCount(updates: PlayerUpdate[]) {
  const key = updates.filter(isKeyUpdate).length
  return key === updates.length
    ? updates.length
    : `${key} key · ${updates.length} total`
}

export function UpdatesSection() {
  const query = useQuery(overviewQueries.updates)
  const updates = query.data?.updates ?? []

  return (
    <Section
      title="Player updates"
      count={query.data ? newsCount(updates) : undefined}
      action={
        updates.length > previewCount && <PlayerNewsSheet updates={updates} />
      }
    >
      {query.isPending ? (
        <SectionLoading />
      ) : query.isError ? (
        <SectionError
          retry={() => {
            void query.refetch()
          }}
        />
      ) : (
        <>
          {updates.length ? (
            <PlayerUpdateList updates={updates.slice(0, previewCount)} />
          ) : (
            <SectionMessage>No recent news for your players.</SectionMessage>
          )}
          <CoverageNote coverage={query.data.coverage} />
        </>
      )}
    </Section>
  )
}
