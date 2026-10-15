// The win-chance graph's time axis covers the whole matchup week, squeezed
// down to game windows: hours with no NFL games take no width.

export type GameWindow = { start: number; end: number; label: string }

// Kickoffs this close together share a window, e.g. a Sunday 1 PM slate.
const SAME_SLATE_MS = 2 * 60 * 60 * 1000
// Long enough for an overtime game plus the final stat updates.
const GAME_LENGTH_MS = 3.75 * 60 * 60 * 1000

export function slateLabel(at: number) {
  const date = new Date(at)
  const day = date.toLocaleDateString(undefined, { weekday: "short" })
  const time = date
    .toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })
    .replace(":00", "")
  return `${day} ${time}`
}

// A saved point this close to a window widens it; one further away, like a
// Thursday-night sample when none of the starters play Thursday, gets its
// own short window instead of stretching a window across the days between.
const POINT_JOIN_MS = 60 * 60 * 1000
const POINT_WINDOW_MS = 30 * 60 * 1000

/**
 * Windows from the week's kickoffs, widened to cover every saved point so
 * none falls between windows.
 */
export function gameWindows(
  kickoffs: number[],
  points: number[],
): GameWindow[] {
  let windows: GameWindow[] = []
  for (const kickoff of [...new Set(kickoffs)].sort((a, b) => a - b)) {
    const last = windows.at(-1)
    if (last && (kickoff - last.start <= SAME_SLATE_MS || kickoff < last.end)) {
      last.end = Math.max(last.end, kickoff + GAME_LENGTH_MS)
      continue
    }
    windows.push({
      start: kickoff,
      end: kickoff + GAME_LENGTH_MS,
      label: slateLabel(kickoff),
    })
  }
  for (const at of [...points].sort((a, b) => a - b)) {
    if (windows.some((w) => at >= w.start && at <= w.end)) continue
    const near = windows.find(
      (w) => at >= w.start - POINT_JOIN_MS && at <= w.end + POINT_JOIN_MS,
    )
    if (near) {
      near.start = Math.min(near.start, at)
      near.end = Math.max(near.end, at)
      continue
    }
    windows.push({
      start: at,
      end: at + POINT_WINDOW_MS,
      label: slateLabel(at),
    })
    windows.sort((a, b) => a.start - b.start)
  }
  windows = windows.reduce<GameWindow[]>((merged, w) => {
    const last = merged.at(-1)
    if (last && w.start <= last.end) last.end = Math.max(last.end, w.end)
    else merged.push({ ...w })
    return merged
  }, [])
  return windows
}

export type TimeAxis = {
  windows: GameWindow[]
  // Position from 0 to 1 across the squeezed week.
  x: (at: number) => number
  // Each window's left edge, from 0 to 1.
  edges: number[]
}

export function timeAxis(windows: GameWindow[]): TimeAxis {
  const lengths = windows.map((w) => Math.max(w.end - w.start, 1))
  const total = lengths.reduce((sum, length) => sum + length, 0)
  const edges: number[] = []
  let offset = 0
  for (const length of lengths) {
    edges.push(total ? offset / total : 0)
    offset += length
  }
  return {
    windows,
    edges,
    x(at) {
      if (!windows.length) return 0
      const i = windows.findIndex((w) => at <= w.end)
      if (i < 0) return 1
      const w = windows[i]
      const within = Math.min(Math.max(at - w.start, 0), lengths[i])
      return Math.min(1, Math.max(0, edges[i] + within / total))
    },
  }
}
