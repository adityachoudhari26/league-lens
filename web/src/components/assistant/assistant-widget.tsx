import { useEffect, useRef, useState } from "react"
import { useRouterState } from "@tanstack/react-router"
import { Sparkles } from "lucide-react"
import { useAssistantSettings } from "@/lib/api/assistant"
import { useAssistantChat } from "./assistant-chat"
import { useAssistantPageContext } from "./assistant-context"
import { AssistantPanel } from "./assistant-panel"

export function AssistantWidget() {
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const page = useAssistantPageContext()
  const settings = useAssistantSettings()
  const chat = useAssistantChat(page)
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    function handlePointerDown(event: PointerEvent) {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("pointerdown", handlePointerDown)
    document.addEventListener("keydown", handleKeyDown)
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown)
      document.removeEventListener("keydown", handleKeyDown)
    }
  }, [open])
  if (pathname === "/connect") return null

  const notSetUp =
    chat.requiresSetup || (settings.isSuccess && !settings.data?.settings)
  return (
    <div ref={rootRef} className="[&_button:not(:disabled)]:cursor-pointer">
      <button
        type="button"
        aria-label="Open Playbook"
        title="Playbook"
        aria-expanded={open}
        aria-controls="assistant-panel"
        className="playbook-ring fixed right-4 bottom-4 z-50 grid size-14 place-items-center rounded-full border-[3px] text-teal-700 shadow-lg transition-transform outline-none hover:scale-105 focus-visible:ring-3 focus-visible:ring-ring/50 dark:text-teal-300"
        onClick={() => setOpen((current) => !current)}
      >
        <Sparkles aria-hidden="true" className="size-6" />
      </button>
      {open ? (
        <AssistantPanel
          messages={chat.messages}
          draft={chat.draft}
          error={chat.error}
          isLoadingSettings={settings.isPending}
          hasSettingsError={settings.isError}
          isNotSetUp={notSetUp}
          isStreaming={chat.isStreaming}
          onDraftChange={chat.setDraft}
          onSend={() => void chat.send()}
          onSuggestion={(value) => void chat.send(value)}
          onStop={chat.stop}
          onNewChat={chat.clear}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  )
}
