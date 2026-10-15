import { useEffect, useRef, useState } from "react"
import type { FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { InlineError } from "./inline-error"
import { InstructionStep, InstructionSteps } from "./instruction-steps"
import { isMac } from "./platform"
import type { ESPNCredentials } from "./types"

export function EspnCookieOption({
  initialCredentials,
  error,
  pending,
  formResetVersion,
  clearVersion,
  onSubmit,
}: {
  initialCredentials?: ESPNCredentials
  error: string
  pending: boolean
  formResetVersion: number
  clearVersion: number
  onSubmit: (credentials: ESPNCredentials) => Promise<boolean>
}) {
  const [s2, setS2] = useState(initialCredentials?.s2 ?? "")
  const [swid, setSwid] = useState(initialCredentials?.swid ?? "")
  const developerToolsShortcut = isMac ? "⌥⌘I" : "F12"
  const previousResetVersions = useRef({ formResetVersion, clearVersion })

  useEffect(() => {
    if (
      previousResetVersions.current.formResetVersion === formResetVersion &&
      previousResetVersions.current.clearVersion === clearVersion
    ) {
      return
    }
    previousResetVersions.current = { formResetVersion, clearVersion }
    setS2("")
    setSwid("")
  }, [formResetVersion, clearVersion])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (await onSubmit({ s2, swid })) {
      setS2("")
      setSwid("")
    }
  }

  return (
    <section className="grid gap-3 rounded-lg border px-4 py-3">
      <h3 className="text-sm font-medium">Option 2: Paste your cookies</h3>
      <InstructionSteps>
        <InstructionStep number={1} title="Open developer tools on espn.com">
          While signed in, press {developerToolsShortcut}.
        </InstructionStep>
        <InstructionStep number={2} title="Find the cookies">
          Go to Application → Cookies → https://www.espn.com. In Firefox:
          Storage → Cookies.
        </InstructionStep>
        <InstructionStep number={3} title="Copy two values">
          Copy the Value of <code>espn_s2</code> and <code>SWID</code> into the
          fields below.
        </InstructionStep>
      </InstructionSteps>
      <form
        className="grid gap-3"
        onSubmit={(event) => void handleSubmit(event)}
      >
        <label className="grid gap-1.5 text-sm" htmlFor="espn-s2">
          espn_s2
          <Input
            id="espn-s2"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={s2}
            onChange={(event) => setS2(event.target.value)}
            required
          />
        </label>
        <label className="grid gap-1.5 text-sm" htmlFor="espn-swid">
          SWID
          <Input
            id="espn-swid"
            type="text"
            autoComplete="off"
            spellCheck={false}
            value={swid}
            onChange={(event) => setSwid(event.target.value)}
            required
          />
        </label>
        <p className="text-xs text-muted-foreground">
          League Lens stores these encrypted on this computer and never shows
          them again.
        </p>
        {error && <InlineError>{error}</InlineError>}
        <Button type="submit" disabled={pending || !s2.trim() || !swid.trim()}>
          {pending ? "Connecting…" : "Find leagues"}
        </Button>
      </form>
    </section>
  )
}
