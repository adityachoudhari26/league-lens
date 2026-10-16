import { timestampMs } from "@bufbuild/protobuf/wkt"
import { useQuery } from "@tanstack/react-query"
import {
  useId,
  useMemo,
  useState,
  type CSSProperties,
  type PointerEvent,
  type ReactNode,
} from "react"
import { initials } from "@/components/player-card/format"
import { teamColor } from "@/components/player-card/team-colors"
import { roundStatus } from "@/components/matchups/rules"
import {
  clockLabel,
  gameWindows,
  timeAxis,
  type TimeAxis,
} from "@/components/matchups/win-chance-axis"
import { Button } from "@/components/ui/button"
import {
  KeyMomentKind,
  type MatchupDetail,
  type MatchupWinChance,
} from "@/gen/leaguelens/v1/matchups_pb"
import { matchupQueries } from "@/lib/api/matchups"
import { playerCardQuery } from "@/lib/api/player-card"

type Sample = { at: number; x: number; p: number }
type Moment = {
  x: number
  // The owner's win chance, rounded, at the samples around the moment.
  before: number
  after: number
  player: string
  playerId: string
  proTeam: string
  // Point change; negative is a drop.
  delta: number
  mine: boolean
  time: string
  // Touchdowns and field goals get a dot on the graph; other plays are only
  // listed, and only when they moved the win chance (MIN_PLAY_SWING).
  kind: "td" | "fg" | "play"
}
type Graph = { axis: TimeAxis; samples: Sample[]; moments: Moment[] }

// What the chart points at: a snapped key moment, a sample under the cursor,
// or nothing (the latest sample is shown).
type ChartFocus =
  { kind: "moment"; index: number } | { kind: "sample"; index: number } | null

const yesTone = "text-teal-700/80 dark:text-[#86d9b3]"
const noTone = "text-red-700/80 dark:text-[#f09591]"
const toneOf = (p: number) => (p >= 50 ? yesTone : noTone)
// Green when the change helped you: your player gained or theirs dropped.
const helped = (m: Moment) => m.mine === m.delta >= 0
const momentTone = (m: Moment) => (helped(m) ? yesTone : noTone)
// The graph uses softer light-mode colors than the text.
const yesGraph = "text-[#64a394] dark:text-[#86d9b3]"
const noGraph = "text-[#cf8e88] dark:text-[#f09591]"
const momentGraphTone = (m: Moment) => (helped(m) ? yesGraph : noGraph)
const swingTone = (swing: number) =>
  swing > 0 ? yesTone : swing < 0 ? noTone : "text-muted-foreground"
const signed = (n: number, digits = 0) =>
  `${n > 0 ? "+" : ""}${n.toFixed(digits)}`

const SNAP_PX = 10
const BUILDING_POLL_MS = 1_000
const LIVE_POLL_MS = 30_000
// Plays (not TDs or FGs) that moved the win chance less than this many
// percentage points are left out. Set from weeks 1–5 of real leagues: it
// keeps about a third of plays, nearly all of them in close games.
const MIN_PLAY_SWING = 2

const momentKinds: Record<KeyMomentKind, Moment["kind"]> = {
  [KeyMomentKind.UNSPECIFIED]: "play",
  [KeyMomentKind.TOUCHDOWN]: "td",
  [KeyMomentKind.FIELD_GOAL]: "fg",
  [KeyMomentKind.PLAY]: "play",
}

function buildGraph(
  matchup: MatchupDetail,
  winChance: MatchupWinChance,
): Graph {
  const sides = [matchup.team, matchup.opponent]
  const players = sides.flatMap((side) =>
    side ? [...side.starters, ...side.bench] : [],
  )
  const kickoffs = sides
    .flatMap((side) => side?.starters ?? [])
    .flatMap((player) =>
      player.kickoffAt ? [timestampMs(player.kickoffAt)] : [],
    )
  const sampleTimes = winChance.samples.map((s) =>
    s.observedAt ? timestampMs(s.observedAt) : 0,
  )
  const momentTimes = winChance.moments.map((m) =>
    m.observedAt ? timestampMs(m.observedAt) : 0,
  )
  const axis = timeAxis(gameWindows(kickoffs, [...sampleTimes, ...momentTimes]))
  const samples = winChance.samples.map((s, i) => ({
    at: sampleTimes[i],
    x: axis.x(sampleTimes[i]),
    p: s.teamWinChance * 100,
  }))
  const moments = winChance.moments.flatMap((m, i): Moment[] => {
    const at = momentTimes[i]
    const kind = momentKinds[m.kind]
    // A moment is saved in the same tick as the sample that includes it.
    let after = samples.findIndex((s) => s.at >= at)
    if (after < 0) after = samples.length - 1
    const pBefore = samples[Math.max(after - 1, 0)]?.p ?? 50
    const pAfter = samples[after]?.p ?? 50
    if (kind === "play" && Math.abs(pAfter - pBefore) < MIN_PLAY_SWING)
      return []
    return [
      {
        x: axis.x(at),
        before: Math.round(pBefore),
        after: Math.round(pAfter),
        player: m.playerName,
        playerId: m.playerId,
        proTeam: players.find((p) => p.playerId === m.playerId)?.proTeam ?? "",
        delta: m.points,
        mine: m.teamId === matchup.team?.teamId,
        time: clockLabel(at),
        kind,
      },
    ]
  })
  return { axis, samples, moments }
}

function nearestSample(samples: Sample[], x: number) {
  let best = 0
  for (let i = 1; i < samples.length; i++)
    if (Math.abs(samples[i].x - x) < Math.abs(samples[best].x - x)) best = i
  return best
}

// Snaps to a TD or FG when the pointer is within SNAP_PX of its time, at any
// height, so crossing a moment's column is enough; otherwise follows the line.
function focusAt(
  { samples, moments }: Graph,
  x: number,
  width: number,
): ChartFocus {
  let snapped = -1
  let snappedDist = SNAP_PX
  moments.forEach((m, i) => {
    if (m.kind === "play") return
    const dist = Math.abs(m.x - x) * width
    if (dist < snappedDist) {
      snapped = i
      snappedDist = dist
    }
  })
  if (snapped >= 0) return { kind: "moment", index: snapped }
  return { kind: "sample", index: nearestSample(samples, x) }
}

function focusedPoint(
  { samples, moments }: Graph,
  focus: ChartFocus,
  final: boolean,
) {
  const m = focus?.kind === "moment" ? moments[focus.index] : undefined
  if (m) return { x: m.x, p: m.after, label: m.time }
  const s = focus?.kind === "sample" ? samples[focus.index] : samples.at(-1)!
  return {
    x: s.x,
    p: Math.round(s.p),
    label: focus ? clockLabel(s.at) : final ? "Final" : "Now",
  }
}

function WinChanceHeader({
  graph,
  focus,
  final,
}: {
  graph: Graph
  focus: ChartFocus
  final: boolean
}) {
  const point = focusedPoint(graph, focus, final)
  const change = point.p - Math.round(graph.samples[0].p)
  return (
    <div className="mb-3 flex items-baseline justify-between gap-3">
      <div className="min-w-0">
        <h2 className="text-sm font-medium">Win chance</h2>
        <p className="truncate text-xs text-muted-foreground">{point.label}</p>
      </div>
      <p className="shrink-0 text-right tabular-nums">
        <span className={`text-2xl font-semibold ${toneOf(point.p)}`}>
          {point.p}%
        </span>
        <span className="ml-1.5 text-xs text-muted-foreground">
          {signed(change)} since kickoff
        </span>
      </p>
    </div>
  )
}

// Window labels, skipping one that would run into the label before it.
const LABEL_GAP = 0.14

function axisLabels(axis: TimeAxis) {
  const labels: { label: string; x: number }[] = []
  axis.windows.forEach((w, i) => {
    const x = axis.edges[i]
    const last = labels.at(-1)
    if (last && x - last.x < LABEL_GAP) return
    labels.push({ label: w.label, x })
  })
  return labels
}

function linePath(samples: Sample[]) {
  return samples
    .map(
      (s, i) =>
        `${i ? "L" : "M"}${(s.x * 100).toFixed(2)},${(100 - s.p).toFixed(2)}`,
    )
    .join(" ")
}

function WinChanceChart({
  graph,
  focus,
  onFocus,
  final,
}: {
  graph: Graph
  focus: ChartFocus
  onFocus: (focus: ChartFocus) => void
  final: boolean
}) {
  const id = useId()
  const { axis, samples, moments } = graph
  const path = linePath(samples)
  const area = `${path} L${samples.at(-1)!.x * 100},50 L${samples[0].x * 100},50 Z`
  const point = focusedPoint(graph, focus, final)
  const focusedMoment =
    focus?.kind === "moment" ? moments[focus.index] : undefined

  function track(e: PointerEvent<HTMLDivElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const x = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
    onFocus(focusAt(graph, x, rect.width))
  }

  return (
    <div className="@container flex gap-2">
      <div className="flex h-44 flex-col justify-between text-[10px] text-muted-foreground tabular-nums">
        <span className="-mt-1.5">100%</span>
        <span>50%</span>
        <span className="-mb-1.5">0%</span>
      </div>
      <div className="min-w-0 flex-1">
        <div
          className="relative h-44 cursor-crosshair touch-pan-y select-none"
          onPointerMove={track}
          onPointerDown={track}
          onPointerLeave={(e) => e.pointerType === "mouse" && onFocus(null)}
        >
          <svg
            viewBox="0 0 100 100"
            preserveAspectRatio="none"
            className="absolute inset-0 size-full overflow-visible"
          >
            <defs>
              <clipPath id={`${id}above`}>
                <rect x="0" y="0" width="100" height="50" />
              </clipPath>
              <clipPath id={`${id}below`}>
                <rect x="0" y="50" width="100" height="50" />
              </clipPath>
            </defs>
            {axis.edges.slice(1).map((edge) => (
              <line
                key={edge}
                x1={edge * 100}
                x2={edge * 100}
                y1="0"
                y2="100"
                className="stroke-border/70 dark:stroke-border"
                strokeWidth="1"
                vectorEffect="non-scaling-stroke"
              />
            ))}
            <line
              x1="0"
              x2="100"
              y1="50"
              y2="50"
              className="stroke-muted-foreground/40"
              strokeDasharray="4 4"
              strokeWidth="1"
              vectorEffect="non-scaling-stroke"
            />
            <path
              d={area}
              clipPath={`url(#${id}above)`}
              className="fill-[#64a394]/8 dark:fill-[#86d9b3]/10"
            />
            <path
              d={area}
              clipPath={`url(#${id}below)`}
              className="fill-[#cf8e88]/8 dark:fill-[#f09591]/10"
            />
            <path
              d={path}
              clipPath={`url(#${id}above)`}
              fill="none"
              className="stroke-[#64a394] dark:stroke-[#86d9b3]"
              strokeWidth="2"
              vectorEffect="non-scaling-stroke"
              strokeLinejoin="round"
            />
            <path
              d={path}
              clipPath={`url(#${id}below)`}
              fill="none"
              className="stroke-[#cf8e88] dark:stroke-[#f09591]"
              strokeWidth="2"
              vectorEffect="non-scaling-stroke"
              strokeLinejoin="round"
            />
            {focus && (
              <line
                x1={point.x * 100}
                x2={point.x * 100}
                y1="0"
                y2="100"
                className="stroke-foreground/20 dark:stroke-foreground/30"
                strokeWidth="1"
                vectorEffect="non-scaling-stroke"
              />
            )}
          </svg>
          {moments.map((m, i) =>
            m.kind === "play" && focusedMoment !== m ? null : (
              <span
                key={i}
                className={`pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 rounded-full bg-current ring-current/25 transition-[width,height] dark:border-[1.5px] dark:border-card ${momentGraphTone(m)} ${focusedMoment === m ? "size-2.5 ring-[3px] dark:size-3" : "size-1.5 dark:size-2"}`}
                style={{ left: `${m.x * 100}%`, top: `${100 - m.after}%` }}
              />
            ),
          )}
          {!focusedMoment && (
            <span
              className="pointer-events-none absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-foreground ring-2 ring-card"
              style={{ left: `${point.x * 100}%`, top: `${100 - point.p}%` }}
            />
          )}
          {focusedMoment && <FloatingMomentCard moment={focusedMoment} />}
        </div>
        <div className="relative mt-1.5 h-4 overflow-hidden text-[10px] text-muted-foreground">
          {axisLabels(axis).map(({ label, x }) => (
            <span
              key={x}
              className="absolute whitespace-nowrap @max-sm:[&:nth-child(2n)]:hidden"
              style={{ left: `${x * 100 + 0.8}%` }}
            >
              {label}
            </span>
          ))}
        </div>
        {focusedMoment && (
          <div className="mt-3 @sm:hidden">
            <MomentCard moment={focusedMoment} />
          </div>
        )}
      </div>
    </div>
  )
}

// Floats beside the snapped dot on the side with more room, kept inside the
// chart. Narrow charts have no room beside a dot, so the card sits under the
// chart instead.
function FloatingMomentCard({ moment: m }: { moment: Moment }) {
  const right = m.x < 0.5
  const left = right
    ? `clamp(0px, calc(${m.x * 100}% + 1rem), calc(100% - 13rem))`
    : `clamp(0px, calc(${m.x * 100}% - 14rem), calc(100% - 13rem))`
  return (
    <div
      className="pointer-events-none absolute z-10 hidden w-52 animate-in duration-100 fade-in-0 zoom-in-95 @sm:block"
      style={{
        left,
        top: `clamp(-0.5rem, calc(${100 - m.after}% - 1.75rem), calc(100% - 3.5rem))`,
      }}
    >
      <MomentCard moment={m} />
    </div>
  )
}

function KindBadge({ kind }: { kind: Moment["kind"] }) {
  if (kind === "play") return null
  return (
    <span className="shrink-0 rounded bg-muted px-1 text-[10px] font-semibold text-muted-foreground dark:bg-white/[0.06]">
      {kind === "td" ? "TD" : "FG"}
    </span>
  )
}

// The mini player card for a key moment: headshot, team-color accent, and
// what the play did to the score and the win chance.
function MomentCard({ moment: m }: { moment: Moment }) {
  const card = useQuery(playerCardQuery(m.playerId))
  const color = teamColor(card.data?.proTeam || m.proTeam)
  const swing = m.after - m.before
  return (
    <div
      style={color ? ({ "--team": color } as CSSProperties) : undefined}
      className="overflow-hidden rounded-lg bg-popover text-popover-foreground shadow-xl ring-1 shadow-black/15 ring-foreground/15 [--team-accent:var(--team)] dark:bg-[#30343c] dark:shadow-black/60 dark:ring-white/15"
    >
      {color && <div className="h-0.5 bg-(--team-accent)" />}
      <div
        className={`flex items-center gap-2.5 px-2.5 py-2 ${color ? "bg-linear-to-br from-(--team-accent)/22 via-(--team-accent)/8 to-transparent dark:from-(--team-accent)/16 dark:via-(--team-accent)/11 dark:to-(--team-accent)/6" : ""}`}
      >
        <Avatar name={m.player} url={card.data?.headshotUrl} tinted={!!color} />
        <div className="min-w-0 flex-1 text-[11px] text-muted-foreground tabular-nums">
          <p className="flex items-center gap-1.5 text-sm font-semibold text-popover-foreground">
            <span className="truncate">{m.player}</span>
            <KindBadge kind={m.kind} />
          </p>
          <p className="truncate">
            {signed(m.delta, 1)} pts ·{" "}
            <span className={swingTone(swing)}>
              {m.before}→{m.after}% ({signed(swing)})
            </span>
          </p>
        </div>
      </div>
    </div>
  )
}

function Avatar({
  name,
  url,
  tinted,
}: {
  name: string
  url?: string
  tinted: boolean
}) {
  const [failed, setFailed] = useState(false)
  const ring = tinted
    ? "ring-2 ring-(--team-accent)/70"
    : "ring-1 ring-foreground/10"
  if (!url || failed)
    return (
      <div
        className={`flex size-8 shrink-0 items-center justify-center rounded-full bg-background text-xs font-semibold text-muted-foreground ${ring}`}
      >
        {initials(name)}
      </div>
    )
  return (
    <img
      src={url}
      alt=""
      onError={() => setFailed(true)}
      className={`size-8 shrink-0 rounded-full bg-background object-cover object-top ${ring}`}
    />
  )
}

// Key moments newest first, scrolling inside the card. Touchdowns and field
// goals carry a badge and match the dots on the graph.
function KeyMomentsList({
  moments,
  focus,
  onFocus,
}: {
  moments: Moment[]
  focus: ChartFocus
  onFocus: (focus: ChartFocus) => void
}) {
  if (!moments.length)
    return (
      <p className="py-2 text-sm text-muted-foreground">No key moments yet.</p>
    )
  const order = moments.map((_, i) => i).reverse()
  return (
    <ol className="-mx-2 grid max-h-96 overflow-y-auto overscroll-contain pr-1 scrollbar-subtle">
      {order.map((i) => {
        const m = moments[i]
        const swing = m.after - m.before
        const active = focus?.kind === "moment" && focus.index === i
        const scoring = m.kind !== "play"
        return (
          <li
            key={i}
            onPointerEnter={() => onFocus({ kind: "moment", index: i })}
            onPointerLeave={() => onFocus(null)}
            className={`flex items-center gap-3 rounded-md px-2 ${scoring ? "py-2" : "py-1.5"} ${active ? "bg-muted/70 dark:bg-white/[0.05]" : ""}`}
          >
            <span
              className={`shrink-0 rounded-full ${scoring ? `size-2 bg-current ${momentGraphTone(m)}` : "size-1.5 bg-muted-foreground/40"}`}
            />
            <div className="min-w-0 flex-1">
              <p
                className={`flex min-w-0 items-center gap-1.5 text-sm ${scoring ? "font-medium" : ""}`}
              >
                <span className="truncate">{m.player}</span>
                <span className={`shrink-0 tabular-nums ${momentTone(m)}`}>
                  {signed(m.delta, 1)}
                </span>
                <KindBadge kind={m.kind} />
              </p>
              <p className="truncate text-xs text-muted-foreground">
                {m.mine ? "Yours" : "Opp"} · {m.time}
              </p>
            </div>
            <div className="shrink-0 text-right tabular-nums">
              <p
                className={`text-sm ${scoring ? "font-medium" : ""} ${swingTone(swing)}`}
              >
                {signed(swing)}%
              </p>
              <p className="text-[11px] whitespace-nowrap text-muted-foreground">
                {m.before}→{m.after}%
              </p>
            </div>
          </li>
        )
      })}
    </ol>
  )
}

function Panel({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border bg-card px-4 py-3">
      {title && <h2 className="mb-1 text-sm font-medium">{title}</h2>}
      {children}
    </section>
  )
}

function PanelMessage({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <Panel title={title}>
      <div className="py-2 text-sm text-muted-foreground">{children}</div>
    </Panel>
  )
}

function SkeletonPanels() {
  return (
    <div role="status" aria-label="Loading win chance" className="grid gap-4">
      <Panel title="Win chance">
        <div className="mt-2 h-44 animate-pulse rounded-md bg-muted/60" />
        <div className="mt-1.5 h-4" />
      </Panel>
      <Panel title="Key moments">
        <div className="grid gap-2 py-1">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-8 animate-pulse rounded-md bg-muted/60" />
          ))}
        </div>
      </Panel>
    </div>
  )
}

/**
 * The win-chance graph and key moments for one matchup. It runs its own
 * query, separate from the lineups: it polls while a finished week is being
 * rebuilt and every 30s while the round is in progress.
 */
export function MatchupWinChancePanels({
  matchup,
}: {
  matchup: MatchupDetail
}) {
  const status = roundStatus(matchup)
  const query = useQuery({
    ...matchupQueries.winChance(matchup.id),
    refetchInterval: (q) =>
      q.state.data?.building
        ? BUILDING_POLL_MS
        : status === "In progress"
          ? LIVE_POLL_MS
          : false,
  })
  const winChance = query.data?.winChance
  const graph = useMemo(
    () =>
      winChance?.samples.length ? buildGraph(matchup, winChance) : undefined,
    [matchup, winChance],
  )
  const [focus, setFocus] = useState<ChartFocus>(null)

  if (query.isPending) return <SkeletonPanels />
  if (query.isError)
    return (
      <PanelMessage title="Win chance">
        <p role="alert">Couldn’t load the win chance.</p>
        <Button
          variant="outline"
          size="sm"
          className="mt-3"
          onClick={() => void query.refetch()}
        >
          Try again
        </Button>
      </PanelMessage>
    )
  // A finished week rebuilds in about a second (2s on cold ESPN feeds), so it
  // shows the same skeleton as the first load.
  if (query.data.building) return <SkeletonPanels />
  if (!graph)
    return (
      <PanelMessage title="Win chance">
        {status === "Upcoming"
          ? "The graph starts at kickoff."
          : "No graph for this week."}
      </PanelMessage>
    )
  const final = status === "Final"
  return (
    <div className="grid gap-4">
      <Panel>
        <WinChanceHeader graph={graph} focus={focus} final={final} />
        <WinChanceChart
          graph={graph}
          focus={focus}
          onFocus={setFocus}
          final={final}
        />
      </Panel>
      <Panel title="Key moments">
        <KeyMomentsList
          moments={graph.moments}
          focus={focus}
          onFocus={setFocus}
        />
      </Panel>
    </div>
  )
}
