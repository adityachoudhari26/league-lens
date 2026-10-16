import { Link } from "@tanstack/react-router"
import { MessageCirclePlus, X } from "lucide-react"
import { useEffect, useRef } from "react"
import { Button } from "@/components/ui/button"
import type { AssistantTurn } from "./assistant-turns"
import { AssistantComposer } from "./assistant-composer"

const suggestions = [
  "How are my matchups looking?",
  "Any injuries on my teams?",
  "Who should I start this week?",
]

type AssistantPanelProps = {
  messages: AssistantTurn[]
  draft: string
  error?: string
  isLoadingSettings: boolean
  hasSettingsError: boolean
  isNotSetUp: boolean
  isStreaming: boolean
  onDraftChange: (value: string) => void
  onSend: () => void
  onSuggestion: (value: string) => void
  onStop: () => void
  onNewChat: () => void
  onClose: () => void
}

export function AssistantPanel({
  messages,
  draft,
  error,
  isLoadingSettings,
  hasSettingsError,
  isNotSetUp,
  isStreaming,
  onDraftChange,
  onSend,
  onSuggestion,
  onStop,
  onNewChat,
  onClose,
}: AssistantPanelProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" })
  }, [error, messages])

  const canChat = !isLoadingSettings && !hasSettingsError && !isNotSetUp
  return (
    <section
      id="assistant-panel"
      aria-label="Assistant chat panel"
      className="fixed right-4 bottom-20 z-50 flex h-[min(560px,calc(100dvh-6rem))] w-[380px] max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-xl border border-border bg-background shadow-xl max-sm:inset-x-0 max-sm:bottom-0 max-sm:h-[min(560px,100dvh)] max-sm:w-full max-sm:max-w-none max-sm:rounded-b-none"
    >
      <AssistantPanelHeader onNewChat={onNewChat} onClose={onClose} />
      <div
        className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain px-4 py-4 scrollbar-subtle"
        role="log"
        aria-live="polite"
        aria-relevant="additions text"
      >
        <AssistantPanelBody
          messages={messages}
          isLoadingSettings={isLoadingSettings}
          hasSettingsError={hasSettingsError}
          isNotSetUp={isNotSetUp}
          onSuggestion={onSuggestion}
        />
        {error && !isNotSetUp ? (
          <p className="text-sm leading-5 text-destructive" role="alert">
            {error}
          </p>
        ) : null}
        <div ref={bottomRef} />
      </div>
      {canChat ? (
        <AssistantComposer
          draft={draft}
          disabled={!canChat}
          isStreaming={isStreaming}
          onChange={onDraftChange}
          onSend={onSend}
          onStop={onStop}
        />
      ) : null}
    </section>
  )
}

function AssistantPanelHeader({
  onNewChat,
  onClose,
}: {
  onNewChat: () => void
  onClose: () => void
}) {
  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-border px-4">
      <h2 className="text-sm font-semibold">Assistant</h2>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          aria-label="New chat"
          title="New chat"
          onClick={onNewChat}
        >
          <MessageCirclePlus aria-hidden="true" />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          aria-label="Close assistant"
          title="Close"
          onClick={onClose}
        >
          <X aria-hidden="true" />
        </Button>
      </div>
    </header>
  )
}

function AssistantPanelBody({
  messages,
  isLoadingSettings,
  hasSettingsError,
  isNotSetUp,
  onSuggestion,
}: Pick<
  AssistantPanelProps,
  | "messages"
  | "isLoadingSettings"
  | "hasSettingsError"
  | "isNotSetUp"
  | "onSuggestion"
>) {
  if (isLoadingSettings) {
    return (
      <p className="my-auto text-center text-sm text-muted-foreground">
        Loading assistant settings…
      </p>
    )
  }
  if (hasSettingsError) {
    return (
      <p
        className="my-auto text-center text-sm text-muted-foreground"
        role="alert"
      >
        Something went wrong. Try again.
      </p>
    )
  }
  if (isNotSetUp) return <AssistantSetupPrompt />
  if (messages.length === 0) {
    return <AssistantSuggestions onSuggestion={onSuggestion} />
  }
  return messages.map((turn, index) => (
    <AssistantMessage key={`${turn.kind}-${index}`} turn={turn} />
  ))
}

function AssistantSetupPrompt() {
  return (
    <div className="my-auto flex flex-col items-center gap-3 text-center">
      <p className="max-w-64 text-sm leading-6 text-muted-foreground">
        Add an AI key in Settings to use the assistant.
      </p>
      <Button
        type="button"
        variant="outline"
        render={<Link to="/settings" hash="assistant" />}
      >
        Open Settings
      </Button>
    </div>
  )
}

function AssistantSuggestions({
  onSuggestion,
}: {
  onSuggestion: (value: string) => void
}) {
  return (
    <div className="my-auto flex flex-col gap-3">
      <p className="text-center text-sm text-muted-foreground">
        What can I help with?
      </p>
      <div className="flex flex-col items-stretch gap-2">
        {suggestions.map((suggestion) => (
          <Button
            key={suggestion}
            type="button"
            variant="outline"
            size="sm"
            className="h-auto justify-start whitespace-normal py-2 text-left"
            onClick={() => onSuggestion(suggestion)}
          >
            {suggestion}
          </Button>
        ))}
      </div>
    </div>
  )
}

function AssistantMessage({ turn }: { turn: AssistantTurn }) {
  if (turn.kind === "tool") {
    return <p className="text-xs text-muted-foreground">{turn.text}</p>
  }
  if (turn.role === "user") {
    return (
      <div className="flex justify-end">
        <p className="max-w-[85%] rounded-2xl bg-muted px-3 py-2 text-sm leading-5">
          {turn.text}
        </p>
      </div>
    )
  }
  return (
    <p className="max-w-full text-sm leading-6 whitespace-pre-wrap">
      {turn.text}
    </p>
  )
}
