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
import { savedKeyMatches } from "./saved-key"

type AssistantModelRequest = Parameters<
  ReturnType<typeof useListAssistantModels>["mutateAsync"]
>[0]

function useAssistantModelCheck() {
  const mutation = useListAssistantModels()
  const [models, setModels] = useState<string[] | null>(null)
  const [error, setError] = useState("")

  async function checkKey(
    request: AssistantModelRequest,
    currentModel: string,
    onModelChange: (model: string) => void,
  ) {
    setError("")
    setModels(null)
    try {
      const result = await mutation.mutateAsync(request)
      setModels(result.models)
      if (result.models.length > 0 && !result.models.includes(currentModel)) {
        onModelChange(result.models[0])
      }
    } catch (cause) {
      setError(assistantErrorMessage(cause))
    }
  }

  return {
    models,
    error,
    isChecking: mutation.isPending,
    checkKey,
  }
}

function ModelField({
  models,
  model,
  onModelChange,
}: {
  models: string[]
  model: string
  onModelChange: (model: string) => void
}) {
  if (models.length > 0) {
    return (
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
    )
  }
  return (
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
  )
}

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
  const { models, error, isChecking, checkKey } = useAssistantModelCheck()
  const hasSavedKey = savedKeyMatches(savedSettings, provider, baseUrl)

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="outline"
          disabled={isChecking}
          onClick={() =>
            void checkKey({ provider, baseUrl, apiKey }, model, onModelChange)
          }
        >
          {isChecking ? "Checking…" : "Check key"}
        </Button>
        {hasSavedKey && !apiKey && (
          <span className="text-xs text-muted-foreground">
            Uses the saved key for this provider and URL.
          </span>
        )}
      </div>
      {error && <InlineError>{error}</InlineError>}
      {models !== null && (
        <ModelField
          models={models}
          model={model}
          onModelChange={onModelChange}
        />
      )}
    </div>
  )
}
