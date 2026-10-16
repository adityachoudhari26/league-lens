import { useState } from "react"
import type { AssistantSettings as AssistantSettingsData } from "@/gen/leaguelens/v1/assistant_pb"
import {
  assistantErrorMessage,
  useAssistantSettings,
  useDeleteAssistantSettings,
  useSaveAssistantSettings,
} from "@/lib/api/assistant"
import { InlineError } from "@/components/connect/inline-error"
import { Skeleton } from "@/components/ui/skeleton"
import { AssistantSettingsForm } from "./assistant-settings-form"
import { AssistantSettingsSummary } from "./assistant-settings-summary"

export function AssistantSettings() {
  const query = useAssistantSettings()
  const saveMutation = useSaveAssistantSettings()
  const deleteMutation = useDeleteAssistantSettings()
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState("")
  const settings = query.data?.settings

  async function save(values: {
    provider: AssistantSettingsData["provider"]
    baseUrl: string
    model: string
    apiKey: string
  }) {
    setError("")
    try {
      await saveMutation.mutateAsync(values)
      setEditing(false)
    } catch (cause) {
      setError(assistantErrorMessage(cause))
    }
  }

  async function remove() {
    if (!window.confirm("Remove your saved AI assistant settings?")) return
    setError("")
    try {
      await deleteMutation.mutateAsync()
      setEditing(false)
    } catch (cause) {
      setError(assistantErrorMessage(cause))
    }
  }

  return (
    <section
      id="assistant"
      className="max-w-2xl rounded-xl border bg-card p-6 sm:p-8"
      aria-labelledby="assistant-heading"
    >
      <header className="mb-6">
        <h2 id="assistant-heading" className="text-base font-medium">
          AI assistant
        </h2>
        <p className="mt-1.5 text-sm text-muted-foreground">
          Bring your own key. League Lens sends your questions and the league
          data needed to answer them to the provider you choose. The key is
          encrypted on this machine and never shown again.
        </p>
      </header>
      <div className="grid gap-3">
        {query.isPending ? (
          <Skeleton className="h-16 w-full rounded-lg" />
        ) : query.isError ? (
          <InlineError>
            Couldn’t load assistant settings. Try again.
          </InlineError>
        ) : settings && !editing ? (
          <AssistantSettingsSummary
            settings={settings}
            onChange={() => setEditing(true)}
            onRemove={() => void remove()}
            removing={deleteMutation.isPending}
          />
        ) : (
          <AssistantSettingsForm
            key={editing ? "editing" : "new"}
            settings={settings}
            saving={saveMutation.isPending}
            onCancel={settings ? () => setEditing(false) : undefined}
            onSave={save}
          />
        )}
        {error && <InlineError>{error}</InlineError>}
      </div>
    </section>
  )
}
