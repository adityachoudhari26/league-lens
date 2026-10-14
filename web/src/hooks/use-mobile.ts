import { useSyncExternalStore } from "react"

const MOBILE_QUERY = "(max-width: 767px)"

function subscribeToViewport(onChange: () => void) {
  const media = window.matchMedia(MOBILE_QUERY)
  media.addEventListener("change", onChange)
  return () => media.removeEventListener("change", onChange)
}

function isMobileViewport() {
  return window.matchMedia(MOBILE_QUERY).matches
}

function serverViewport() {
  return false
}

export function useIsMobile() {
  return useSyncExternalStore(
    subscribeToViewport,
    isMobileViewport,
    serverViewport,
  )
}
