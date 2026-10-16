import { useState } from "react"
import {
  AssistantProvider,
  type AssistantSettings,
} from "@/gen/leaguelens/v1/assistant_pb"
import { Button } from "@/components/ui/button"
import { AssistantModelPicker } from "./assistant-model-picker"
import { AssistantProviderFields } from "./assistant-provider-fields"

export function AssistantSettingsForm({
  settings,
  saving,
  onCancel,
  onSave,
}: {
  settings?: AssistantSettings
  saving: boolean
  onCancel?: () => void
  onSave: (values: {
    provider: AssistantProvider
    baseUrl: string
    model: string
    apiKey: string
  }) => Promise<void>
}) {
  const [provider, setProvider] = useState(
    settings?.provider ?? AssistantProvider.ANTHROPIC,
  )
  const [baseUrl, setBaseUrl] = useState(settings?.baseUrl ?? "")
  const [model, setModel] = useState(settings?.model ?? "")
  const [apiKey, setApiKey] = useState("")
  const [configurationVersion, setConfigurationVersion] = useState(0)

  function changeProvider(nextProvider: AssistantProvider) {
    setProvider(nextProvider)
    setApiKey("")
    if (nextProvider === AssistantProvider.ANTHROPIC) {
      setBaseUrl("")
    } else if (!baseUrl) {
      setBaseUrl("https://api.openai.com/v1")
    }
  }

  return (
    <form
      className="grid gap-4 rounded-lg border px-4 py-3"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave({ provider, baseUrl, model, apiKey })
      }}
    >
      <AssistantProviderFields
        provider={provider}
        baseUrl={baseUrl}
        apiKey={apiKey}
        savedSettings={settings}
        onProviderChange={changeProvider}
        onBaseUrlChange={setBaseUrl}
        onApiKeyChange={setApiKey}
        onConfigurationChange={() =>
          setConfigurationVersion((version) => version + 1)
        }
      />
      <AssistantModelPicker
        key={configurationVersion}
        provider={provider}
        baseUrl={baseUrl}
        apiKey={apiKey}
        savedSettings={settings}
        model={model}
        onModelChange={setModel}
      />
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={saving || !model}>
          {saving ? "Saving…" : "Save"}
        </Button>
        {onCancel && (
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  )
}
