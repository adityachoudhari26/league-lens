import { BackButton } from "./back-button"
import { EspnBookmarkletOption } from "./espn-bookmarklet"
import { EspnCookieOption } from "./espn-cookie-form"
import type { ESPNCredentials } from "./types"

export function EspnStep({
  hidden,
  initialCredentials,
  error,
  pending,
  formResetVersion,
  espnClearVersion,
  onBack,
  onSubmit,
}: {
  hidden: boolean
  initialCredentials?: ESPNCredentials
  error: string
  pending: boolean
  formResetVersion: number
  espnClearVersion: number
  onBack: () => void
  onSubmit: (credentials: ESPNCredentials) => Promise<boolean>
}) {
  return (
    <div hidden={hidden} className={hidden ? "hidden" : "grid gap-5"}>
      <BackButton onClick={onBack}>Back to platforms</BackButton>
      <EspnBookmarkletOption key={hidden ? "inactive" : "active"} />
      <EspnCookieOption
        initialCredentials={initialCredentials}
        error={error}
        pending={pending}
        formResetVersion={formResetVersion}
        clearVersion={espnClearVersion}
        onSubmit={onSubmit}
      />
    </div>
  )
}
