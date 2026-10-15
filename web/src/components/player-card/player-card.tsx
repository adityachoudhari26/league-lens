import { useState, type CSSProperties, type ReactNode } from "react"
import { Popover } from "@base-ui/react/popover"
import { useQuery } from "@tanstack/react-query"
import { cn } from "cn"
import { InjuryBadge } from "@/components/matchups/lineup"
import { injuryLabel } from "@/components/matchups/rules"
import { kickoffTime } from "@/components/overview/format"
import { Skeleton } from "@/components/ui/skeleton"
import {
  PlayerKind,
  type PlayerBlurb,
  type PlayerCard,
  type PlayerStat,
} from "@/gen/leaguelens/v1/dashboard_pb"
import { playerCardQuery } from "@/lib/api/player-card"
import {
  bioLine,
  blurbTitle,
  fallbackLabel,
  identityLine,
  initials,
  ownershipLine,
  statValue,
} from "./format"
import { teamColor } from "./team-colors"

const SHOWN_STATS = 6

// Wraps a player's name so hovering (after a short delay) or tapping it opens
// their info card. The card is fetched only once it opens.
export function PlayerCardTrigger({
  playerId,
  className,
  children,
}: {
  playerId: string
  className?: string
  children: ReactNode
}) {
  return (
    <Popover.Root>
      <Popover.Trigger
        openOnHover
        delay={300}
        className={cn(
          "min-w-0 cursor-pointer truncate rounded-sm text-left underline-offset-4 outline-none hover:underline focus-visible:outline-2 focus-visible:outline-ring data-[popup-open]:underline",
          className,
        )}
      >
        {children}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner
          side="bottom"
          align="start"
          sideOffset={6}
          collisionPadding={8}
          className="isolate z-50 outline-none"
        >
          <Popover.Popup
            data-slot="player-card"
            className="w-80 max-w-[calc(100vw-1rem)] origin-(--transform-origin) overflow-hidden rounded-xl bg-popover text-left text-popover-foreground shadow-xl ring-1 shadow-black/15 ring-foreground/15 duration-100 dark:bg-[#30343c] dark:shadow-black/60 dark:ring-white/15 outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95"
          >
            <PlayerCardBody playerId={playerId} />
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

function PlayerCardBody({ playerId }: { playerId: string }) {
  const query = useQuery(playerCardQuery(playerId))
  if (query.isPending) return <PlayerCardSkeleton />
  if (query.isError || !query.data)
    return (
      <p className="p-3 text-sm text-muted-foreground">Couldn't load player.</p>
    )
  return <PlayerCardDetails card={query.data} />
}

// Mirrors the card's sections so it keeps its size when the data arrives.
function PlayerCardSkeleton() {
  return (
    <div aria-busy="true" aria-label="Loading player">
      <HeaderBand>
        <Skeleton className="size-14 shrink-0 rounded-full bg-foreground/10" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-4 w-36 bg-foreground/10" />
          <Skeleton className="h-3 w-24 bg-foreground/10" />
          <Skeleton className="h-3 w-44 bg-foreground/10" />
        </div>
      </HeaderBand>
      <CardSections>
        <div className="space-y-1.5">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-3.5 w-full" />
          <Skeleton className="h-3.5 w-full" />
          <Skeleton className="h-3.5 w-3/4" />
        </div>
        <div className="grid grid-cols-2 gap-x-3 gap-y-2">
          {Array.from({ length: 4 }, (_, index) => (
            <div key={index} className="space-y-1">
              <Skeleton className="h-2.5 w-20" />
              <Skeleton className="h-4 w-14" />
            </div>
          ))}
        </div>
        <div className="space-y-1">
          <Skeleton className="h-3 w-36" />
          <Skeleton className="h-3 w-28" />
        </div>
      </CardSections>
    </div>
  )
}

// The tinted top strip that sets the card apart from the rows beneath it.
// Inside a team-colored card it fades in from the team's color.
function HeaderBand({
  teamTinted = false,
  children,
}: {
  teamTinted?: boolean
  children: ReactNode
}) {
  return (
    <div
      className={cn(
        "flex items-center gap-3 border-b bg-muted/70 p-3 dark:bg-white/[0.04]",
        teamTinted &&
          "bg-linear-to-br from-(--team-accent)/22 via-(--team-accent)/8 to-transparent dark:from-(--team-accent)/16 dark:via-(--team-accent)/11 dark:to-(--team-accent)/6",
      )}
    >
      {children}
    </div>
  )
}

function CardSections({ children }: { children: ReactNode }) {
  return <div className="divide-y px-3 *:py-2.5">{children}</div>
}

function teamAccentStyle(color: string) {
  return { "--team": color } as CSSProperties
}

function PlayerCardDetails({ card }: { card: PlayerCard }) {
  const color = teamColor(card.proTeam)
  return (
    // --team-accent is the team color, lifted in dark mode so navy and
    // near-black teams stay visible on the dark surface.
    <div
      style={color ? teamAccentStyle(color) : undefined}
      className="[--team-accent:var(--team)] dark:[--team-accent:oklch(from_var(--team)_max(l,0.62)_max(c,min(c*4,0.12))_h)]"
    >
      {color && <div className="h-1 bg-(--team-accent)" />}
      <PlayerCardHeader card={card} teamTinted={color !== undefined} />
      <CardSections>
        {card.blurb && <BlurbSection blurb={card.blurb} />}
        {card.seasonStats.length > 0 && <StatGrid stats={card.seasonStats} />}
        <PlayerCardFooter card={card} />
      </CardSections>
    </div>
  )
}

function PlayerCardHeader({
  card,
  teamTinted,
}: {
  card: PlayerCard
  teamTinted: boolean
}) {
  const injury = injuryLabel(card.injuryStatus)
  const identity = identityLine(card)
  const bio = bioLine(card)
  return (
    <HeaderBand teamTinted={teamTinted}>
      <PlayerPhoto card={card} teamTinted={teamTinted} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <p className="truncate text-sm font-semibold">{card.playerName}</p>
          {injury && <InjuryBadge status={injury} />}
        </div>
        {identity && (
          <p className="truncate text-xs text-muted-foreground">{identity}</p>
        )}
        {bio && (
          <p className="line-clamp-2 text-xs text-muted-foreground">{bio}</p>
        )}
      </div>
    </HeaderBand>
  )
}

// A headshot for athletes and a team logo for D/ST, or initials without one.
function PlayerPhoto({
  card,
  teamTinted,
}: {
  card: PlayerCard
  teamTinted: boolean
}) {
  const [failed, setFailed] = useState(false)
  const logo = card.playerKind === PlayerKind.TEAM_DEFENSE
  if (!card.headshotUrl || failed)
    return (
      <div className="flex size-14 shrink-0 items-center justify-center rounded-full bg-background text-sm ring-1 ring-foreground/10 font-semibold text-muted-foreground">
        {initials(card.playerName)}
      </div>
    )
  return (
    <img
      src={card.headshotUrl}
      alt=""
      onError={() => setFailed(true)}
      className={cn(
        "size-14 shrink-0 rounded-full bg-background ring-1 ring-foreground/10",
        teamTinted && "ring-2 ring-(--team-accent)/70",
        logo ? "object-contain p-1.5" : "object-cover object-top",
      )}
    />
  )
}

function BlurbSection({ blurb }: { blurb: PlayerBlurb }) {
  const title = blurbTitle(blurb)
  return (
    <div>
      {title && (
        <p className="mb-1 text-xs font-medium text-muted-foreground">
          {title}
        </p>
      )}
      <p className="line-clamp-4 text-sm leading-5">{blurb.text}</p>
      {blurb.url && (
        <a
          className="mt-1 inline-block text-xs font-medium text-primary underline-offset-4 hover:underline"
          href={blurb.url}
          target="_blank"
          rel="noreferrer"
        >
          Read more
        </a>
      )}
    </div>
  )
}

function StatGrid({ stats }: { stats: PlayerStat[] }) {
  return (
    <dl className="grid grid-cols-2 gap-x-3 gap-y-2">
      {stats.slice(0, SHOWN_STATS).map((stat) => (
        <div key={stat.label} className="min-w-0">
          <dt className="truncate text-[11px] text-muted-foreground">
            {stat.label}
          </dt>
          <dd className="text-sm font-medium tabular-nums">
            {statValue(stat)}
            {stat.rank !== undefined && (
              <span className="ml-1 text-[11px] font-normal text-muted-foreground">
                #{stat.rank}
              </span>
            )}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function PlayerCardFooter({ card }: { card: PlayerCard }) {
  const nextGame = card.nextGame
  const kickoff = kickoffTime(nextGame?.kickoffAt)
  const ownership = ownershipLine(card)
  const fallback = fallbackLabel(card.profileFetchedAt)
  if (!nextGame && !ownership && !fallback) return null
  return (
    <div className="space-y-0.5 text-xs text-muted-foreground">
      {nextGame && (
        <p>
          Next: vs {nextGame.opponent}
          {kickoff && ` · ${kickoff}`}
        </p>
      )}
      {ownership && <p>{ownership}</p>}
      {fallback && <p>{fallback}</p>}
    </div>
  )
}
