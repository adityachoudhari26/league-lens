import type { FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { BackButton } from "./back-button"
import { InlineError } from "./inline-error"

export function SleeperStep({
  username,
  onUsernameChange,
  error,
  pending,
  onBack,
  onSubmit,
}: {
  username: string
  onUsernameChange: (username: string) => void
  error: string
  pending: boolean
  onBack: () => void
  onSubmit: (username: string) => Promise<void>
}) {
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await onSubmit(username)
  }

  return (
    <form className="grid gap-4" onSubmit={(event) => void handleSubmit(event)}>
      <BackButton onClick={onBack}>Back to platforms</BackButton>
      <label className="grid gap-1.5 text-sm" htmlFor="sleeper-username">
        Sleeper username
        <Input
          id="sleeper-username"
          autoComplete="username"
          value={username}
          onChange={(event) => onUsernameChange(event.target.value)}
          required
        />
      </label>
      {error && <InlineError>{error}</InlineError>}
      <Button type="submit" disabled={pending || !username.trim()}>
        {pending ? "Finding leagues…" : "Find leagues"}
      </Button>
    </form>
  )
}
