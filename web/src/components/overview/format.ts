import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"

export function observedTime(value?: Timestamp) {
  return value
    ? timestampDate(value).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      })
    : "Unknown"
}

// Unknown scores must not look like actual zero-point performances.
export function scoreLabel(value?: number) {
  return value === undefined
    ? "—"
    : value.toLocaleString(undefined, {
        minimumFractionDigits: 1,
        maximumFractionDigits: 2,
      })
}

export function kickoffTime(value?: Timestamp) {
  return value
    ? timestampDate(value).toLocaleString(undefined, {
        weekday: "short",
        hour: "numeric",
        minute: "2-digit",
      })
    : undefined
}
