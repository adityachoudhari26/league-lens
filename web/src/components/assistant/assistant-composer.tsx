import { Send, Square } from "lucide-react"
import { useEffect, useRef, type KeyboardEvent } from "react"
import { Button } from "@/components/ui/button"

type AssistantComposerProps = {
  draft: string
  disabled: boolean
  isStreaming: boolean
  onChange: (value: string) => void
  onSend: () => void
  onStop: () => void
}

export function AssistantComposer({
  draft,
  disabled,
  isStreaming,
  onChange,
  onSend,
  onStop,
}: AssistantComposerProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (draft === "" && textareaRef.current) {
      textareaRef.current.style.height = "auto"
    }
  }, [draft])

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey && !disabled && !isStreaming) {
      event.preventDefault()
      onSend()
    }
  }

  return (
    <div className="flex shrink-0 items-end gap-2 border-t border-border p-3">
      <label className="sr-only" htmlFor="assistant-message">
        Message Playbook
      </label>
      <textarea
        ref={textareaRef}
        id="assistant-message"
        rows={1}
        maxLength={4000}
        value={draft}
        disabled={disabled || isStreaming}
        onChange={(event) => {
          onChange(event.currentTarget.value)
          event.currentTarget.style.height = "auto"
          event.currentTarget.style.height = `${Math.min(event.currentTarget.scrollHeight, 120)}px`
        }}
        onKeyDown={handleKeyDown}
        placeholder="Ask about your leagues…"
        className="max-h-[7.5rem] min-h-9 min-w-0 flex-1 resize-none overflow-y-auto rounded-lg border border-input bg-background px-3 py-2 text-sm leading-5 shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30 disabled:opacity-60"
      />
      {isStreaming ? (
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label="Stop assistant response"
          title="Stop"
          onClick={onStop}
        >
          <Square aria-hidden="true" />
        </Button>
      ) : (
        <Button
          type="button"
          size="icon"
          aria-label="Send message"
          title="Send"
          className="playbook-gradient text-white hover:opacity-90"
          disabled={disabled || !draft.trim()}
          onClick={onSend}
        >
          <Send aria-hidden="true" />
        </Button>
      )}
    </div>
  )
}
