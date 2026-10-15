import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"
import {
  PlayerBlurbKind,
  PlayerPosition,
  type PlayerBlurb,
  type PlayerCard,
  type PlayerDraft,
  type PlayerStat,
} from "@/gen/leaguelens/v1/players_pb"

// A saved profile older than this means the live fetch failed and the card
// shows the last one that worked.
const FALLBACK_AGE_MS = 10 * 60 * 1000

const positionLabels: Record<PlayerPosition, string | undefined> = {
  [PlayerPosition.UNSPECIFIED]: undefined,
  [PlayerPosition.QB]: "QB",
  [PlayerPosition.RB]: "RB",
  [PlayerPosition.WR]: "WR",
  [PlayerPosition.TE]: "TE",
  [PlayerPosition.K]: "K",
  [PlayerPosition.DEF]: "D/ST",
}

export function positionLabel(position: PlayerPosition) {
  return positionLabels[position]
}

// "RB · ATL #7 · RB3": position, NFL team with jersey, and position rank.
export function identityLine(card: PlayerCard) {
  const position = positionLabel(card.position)
  const team = [card.proTeam, card.jersey && `#${card.jersey}`]
    .filter(Boolean)
    .join(" ")
  const rank =
    card.positionRank !== undefined
      ? `${position ?? "#"}${card.positionRank}`
      : undefined
  return [position, team, rank].filter(Boolean).join(" · ")
}

// "24 · 5'11\" 215 · Texas · 2023 R1 P8"
export function bioLine(card: PlayerCard) {
  const build = [heightLabel(card.heightInches), card.weightLbs]
    .filter((value) => value !== undefined)
    .join(" ")
  return [card.age, build, card.college, card.draft && draftLabel(card.draft)]
    .filter((value) => value !== undefined && value !== "")
    .join(" · ")
}

function heightLabel(inches?: number) {
  if (inches === undefined) return undefined
  return `${Math.floor(inches / 12)}'${inches % 12}"`
}

function draftLabel(draft: PlayerDraft) {
  return `${draft.year} R${draft.round} P${draft.pick}`
}

export function blurbTitle(blurb: PlayerBlurb) {
  switch (blurb.kind) {
    case PlayerBlurbKind.WEEKLY_OUTLOOK:
      return `Week ${blurb.week} outlook`
    case PlayerBlurbKind.NEWS:
      return blurb.publishedAt
        ? `News · ${relativeTime(blurb.publishedAt)}`
        : "News"
    case PlayerBlurbKind.SEASON_OUTLOOK:
      return "Season outlook"
    default:
      return undefined
  }
}

// Whole numbers stay whole; rates keep one decimal.
export function statValue(stat: PlayerStat) {
  return stat.value.toLocaleString(undefined, { maximumFractionDigits: 1 })
}

export function ownershipLine(card: PlayerCard) {
  return [
    card.percentRostered !== undefined &&
      `${percentLabel(card.percentRostered)} rostered`,
    card.percentStarted !== undefined &&
      `${percentLabel(card.percentStarted)} started`,
  ]
    .filter(Boolean)
    .join(" · ")
}

function percentLabel(value: number) {
  return `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })}%`
}

// "Updated 3h ago", only when the card fell back to an older saved profile.
export function fallbackLabel(fetchedAt?: Timestamp, now = Date.now()) {
  if (!fetchedAt) return undefined
  if (now - timestampDate(fetchedAt).getTime() < FALLBACK_AGE_MS)
    return undefined
  return `Updated ${relativeTime(fetchedAt, now)}`
}

export function relativeTime(value: Timestamp, now = Date.now()) {
  const minutes = Math.round((now - timestampDate(value).getTime()) / 60000)
  if (minutes < 60) return `${Math.max(minutes, 1)}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}

export function initials(name: string) {
  return name
    .split(/\s+/)
    .map((part) => part[0])
    .filter(Boolean)
    .slice(0, 2)
    .join("")
    .toUpperCase()
}
