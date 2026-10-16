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

function serviceFor(baseUrl: string) {
  return (
    services.find((service) => service.baseUrl === baseUrl)?.value ?? "custom"
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
  const savedKeyMatches =
    savedSettings?.provider === provider && savedSettings.baseUrl === baseUrl

  return (
    <div className="grid gap-3">
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
            if (!value) return
            const nextProvider = Number(value)
            const isOpenAICompatible =
              value === String(AssistantProvider.OPENAI_COMPATIBLE)
            onProviderChange(nextProvider)
            setServiceSelection(isOpenAICompatible ? "openai" : "custom")
            onConfigurationChange()
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

      {openAICompatible && (
        <>
          <div className="grid gap-2 text-sm font-medium">
            <span>Service</span>
            <Select
              items={[...services, { value: "custom", label: "Custom" }]}
              value={serviceSelection}
              onValueChange={(value) => {
                if (!value) return
                if (value === "custom") {
                  setServiceSelection("custom")
                  return
                }
                const service = services.find(
                  (option) => option.value === value,
                )
                if (!service) return
                setServiceSelection(service.value)
                onBaseUrlChange(service.baseUrl)
                onConfigurationChange()
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
              onChange={(event) => {
                setServiceSelection("custom")
                onBaseUrlChange(event.target.value)
                onConfigurationChange()
              }}
              autoComplete="url"
              placeholder="https://api.example.com/v1"
            />
          </div>
          <p className="-mt-1 text-xs text-muted-foreground">
            For local servers, the key is optional. In the Docker install use
            http://host.docker.internal:11434/v1 for Ollama.
          </p>
        </>
      )}

      <div className="grid gap-2 text-sm font-medium">
        <span>API key</span>
        <Input
          aria-label="API key"
          type="password"
          autoComplete="off"
          value={apiKey}
          onChange={(event) => {
            onApiKeyChange(event.target.value)
            onConfigurationChange()
          }}
          placeholder={
            savedKeyMatches
              ? "Leave blank to keep the saved key"
              : openAICompatible
                ? "Optional for local servers"
                : "Paste your API key"
          }
        />
      </div>
    </div>
  )
}
