import {
  AssistantProvider,
  type AssistantSettings as AssistantSettingsData,
} from "@/gen/leaguelens/v1/assistant_pb"
import { Button } from "@/components/ui/button"

export function AssistantSettingsSummary({
  settings,
  onChange,
  onRemove,
  removing,
}: {
  settings: AssistantSettingsData
  onChange: () => void
  onRemove: () => void
  removing: boolean
}) {
  const provider =
    settings.provider === AssistantProvider.ANTHROPIC
      ? "Anthropic"
      : "OpenAI-compatible"
  const keySummary = settings.apiKeyHint
    ? `key ending ${settings.apiKeyHint}`
    : "no key saved"

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
      <p className="min-w-0 break-words text-sm">
        {provider} · {settings.model} · {keySummary}
      </p>
      <div className="flex gap-3">
        <Button variant="outline" onClick={onChange}>
          Change
        </Button>
        <Button variant="outline" disabled={removing} onClick={onRemove}>
          Remove
        </Button>
      </div>
    </div>
  )
}
