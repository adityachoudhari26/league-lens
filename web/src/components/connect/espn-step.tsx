import { BackButton } from "./back-button"
import { EspnBookmarkletOption } from "./espn-bookmarklet"
import { EspnCookieOption } from "./espn-cookie-form"
import type { ESPNCredentials } from "./types"

export function EspnStep({
  initialCredentials,
  error,
  pending,
  onBack,
  onSubmit,
}: {
  initialCredentials?: ESPNCredentials
  error: string
  pending: boolean
  onBack: () => void
  onSubmit: (credentials: ESPNCredentials) => Promise<void>
}) {
  return (
    <div className="grid gap-5">
      <BackButton label="Back to platforms" onClick={onBack} />
      <EspnBookmarkletOption />
      <EspnCookieOption
        initialCredentials={initialCredentials}
        error={error}
        pending={pending}
        onSubmit={onSubmit}
      />
    </div>
  )
}
