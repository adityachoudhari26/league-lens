import { useState } from "react"
import {
  AssistantProvider,
  type AssistantSettings,
} from "@/gen/leaguelens/v1/assistant_pb"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { savedKeyMatches } from "./saved-key"

const services = [
  { value: "openai", label: "OpenAI", baseUrl: "https://api.openai.com/v1" },
  {
    value: "openrouter",
    label: "OpenRouter",
    baseUrl: "https://openrouter.ai/api/v1",
  },
  {
    value: "gemini",
    label: "Google Gemini",
    baseUrl: "https://generativelanguage.googleapis.com/v1beta/openai",
  },
  {
    value: "ollama",
    label: "Ollama",
    baseUrl: "http://localhost:11434/v1",
  },
] as const

type ServiceSelection = (typeof services)[number]["value"] | "custom"

function serviceFor(baseUrl: string): ServiceSelection {
  return (
    services.find((service) => service.baseUrl === baseUrl)?.value ?? "custom"
  )
}

function apiKeyPlaceholder(
  savedKeyMatches: boolean,
  openAICompatible: boolean,
): string {
  if (savedKeyMatches) return "Leave blank to keep the saved key"
  if (openAICompatible) return "Optional for local servers"
  return "Paste your API key"
}

function ProviderSelect({
  provider,
  onChange,
}: {
  provider: AssistantProvider
  onChange: (provider: AssistantProvider) => void
}) {
  return (
    <div className="grid gap-2 text-sm font-medium">
      <span>Provider</span>
      <Select
        items={[
          { value: String(AssistantProvider.ANTHROPIC), label: "Anthropic" },
          {
            value: String(AssistantProvider.OPENAI_COMPATIBLE),
            label: "OpenAI-compatible",
          },
        ]}
        value={String(provider)}
        onValueChange={(value) => {
          if (value) onChange(Number(value))
        }}
      >
        <SelectTrigger aria-label="Provider" className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={String(AssistantProvider.ANTHROPIC)}>
            Anthropic
          </SelectItem>
          <SelectItem value={String(AssistantProvider.OPENAI_COMPATIBLE)}>
            OpenAI-compatible
          </SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}

function OpenAICompatibleFields({
  baseUrl,
  serviceSelection,
  onServiceChange,
  onBaseUrlChange,
}: {
  baseUrl: string
  serviceSelection: ServiceSelection
  onServiceChange: (serviceSelection: ServiceSelection) => void
  onBaseUrlChange: (baseUrl: string) => void
}) {
  return (
    <>
      <div className="grid gap-2 text-sm font-medium">
        <span>Service</span>
        <Select
          items={[...services, { value: "custom", label: "Custom" }]}
          value={serviceSelection}
          onValueChange={(value) => {
            if (value) onServiceChange(value)
          }}
        >
          <SelectTrigger aria-label="Service" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {services.map((service) => (
              <SelectItem key={service.value} value={service.value}>
                {service.label}
              </SelectItem>
            ))}
            <SelectItem value="custom">Custom</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-2 text-sm font-medium">
        <span>Base URL</span>
        <Input
          aria-label="Base URL"
          value={baseUrl}
          onChange={(event) => onBaseUrlChange(event.target.value)}
          autoComplete="url"
          placeholder="https://api.example.com/v1"
        />
      </div>
      <p className="-mt-1 text-xs text-muted-foreground">
        For local servers, the key is optional. In the Docker install use
        http://host.docker.internal:11434/v1 for Ollama.
      </p>
    </>
  )
}

function ApiKeyField({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (value: string) => void
  placeholder: string
}) {
  return (
    <div className="grid gap-2 text-sm font-medium">
      <span>API key</span>
      <Input
        aria-label="API key"
        type="password"
        autoComplete="off"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
      />
    </div>
  )
}

export function AssistantProviderFields({
  provider,
  baseUrl,
  apiKey,
  savedSettings,
  onProviderChange,
  onBaseUrlChange,
  onApiKeyChange,
  onConfigurationChange,
}: {
  provider: AssistantProvider
  baseUrl: string
  apiKey: string
  savedSettings?: AssistantSettings
  onProviderChange: (provider: AssistantProvider) => void
  onBaseUrlChange: (baseUrl: string) => void
  onApiKeyChange: (apiKey: string) => void
  onConfigurationChange: () => void
}) {
  const [serviceSelection, setServiceSelection] = useState(serviceFor(baseUrl))
  const openAICompatible = provider === AssistantProvider.OPENAI_COMPATIBLE
  const hasSavedKey = savedKeyMatches(savedSettings, provider, baseUrl)

  function handleProviderChange(nextProvider: AssistantProvider) {
    onProviderChange(nextProvider)
    const isOpenAICompatible =
      nextProvider === AssistantProvider.OPENAI_COMPATIBLE
    setServiceSelection(isOpenAICompatible ? "openai" : "custom")
    onConfigurationChange()
  }

  function handleServiceChange(selection: ServiceSelection) {
    if (selection === "custom") {
      setServiceSelection("custom")
      return
    }
    const service = services.find((option) => option.value === selection)
    if (!service) return
    setServiceSelection(service.value)
    onBaseUrlChange(service.baseUrl)
    onConfigurationChange()
  }

  function handleBaseUrlChange(nextBaseUrl: string) {
    setServiceSelection("custom")
    onBaseUrlChange(nextBaseUrl)
    onConfigurationChange()
  }

  function handleApiKeyChange(nextApiKey: string) {
    onApiKeyChange(nextApiKey)
    onConfigurationChange()
  }

  return (
    <div className="grid gap-3">
      <ProviderSelect provider={provider} onChange={handleProviderChange} />
      {openAICompatible && (
        <OpenAICompatibleFields
          baseUrl={baseUrl}
          serviceSelection={serviceSelection}
          onServiceChange={handleServiceChange}
          onBaseUrlChange={handleBaseUrlChange}
        />
      )}
      <ApiKeyField
        value={apiKey}
        onChange={handleApiKeyChange}
        placeholder={apiKeyPlaceholder(hasSavedKey, openAICompatible)}
      />
    </div>
  )
}
