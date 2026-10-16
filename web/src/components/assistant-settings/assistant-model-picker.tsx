import { useState } from "react"
import type {
  AssistantProvider,
  AssistantSettings,
} from "@/gen/leaguelens/v1/assistant_pb"
import {
  assistantErrorMessage,
  useListAssistantModels,
} from "@/lib/api/assistant"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { InlineError } from "@/components/connect/inline-error"

export function AssistantModelPicker({
  provider,
  baseUrl,
  apiKey,
  savedSettings,
  model,
  onModelChange,
}: {
  provider: AssistantProvider
  baseUrl: string
  apiKey: string
  savedSettings?: AssistantSettings
  model: string
  onModelChange: (model: string) => void
}) {
  const mutation = useListAssistantModels()
  const [models, setModels] = useState<string[] | null>(null)
  const [error, setError] = useState("")

  async function checkKey() {
    setError("")
    setModels(null)
    try {
      const result = await mutation.mutateAsync({
        provider,
        baseUrl,
        apiKey,
      })
      setModels(result.models)
      if (result.models.length > 0 && !result.models.includes(model)) {
        onModelChange(result.models[0])
      }
    } catch (cause) {
      setError(assistantErrorMessage(cause))
    }
  }

  const savedKeyMatches =
    savedSettings?.provider === provider && savedSettings.baseUrl === baseUrl

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="outline"
          disabled={mutation.isPending}
          onClick={() => void checkKey()}
        >
          {mutation.isPending ? "Checking…" : "Check key"}
        </Button>
        {savedKeyMatches && !apiKey && (
          <span className="text-xs text-muted-foreground">
            Uses the saved key for this provider and URL.
          </span>
        )}
      </div>
      {error && <InlineError>{error}</InlineError>}
      {models !== null &&
        (models.length > 0 ? (
          <div className="grid gap-2 text-sm font-medium">
            <span>Model</span>
            <Select
              items={models.map((modelId) => ({
                value: modelId,
                label: modelId,
              }))}
              value={model}
              onValueChange={(value) => value && onModelChange(value)}
            >
              <SelectTrigger aria-label="Model" className="w-full">
                <SelectValue placeholder="Choose a model" />
              </SelectTrigger>
              <SelectContent>
                {models.map((modelId) => (
                  <SelectItem key={modelId} value={modelId}>
                    {modelId}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : (
          <div className="grid gap-2 text-sm font-medium">
            <span>Model</span>
            <Input
              aria-label="Model"
              value={model}
              onChange={(event) => onModelChange(event.target.value)}
              placeholder="Enter a model ID"
              autoComplete="off"
            />
          </div>
        ))}
    </div>
  )
}
