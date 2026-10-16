import { useState } from "react"
import { useRouterState } from "@tanstack/react-router"
import { RugbyBall } from "lucide-react"
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
  if (pathname === "/connect") return null

  const notSetUp =
    chat.requiresSetup || (settings.isSuccess && !settings.data?.settings)
  return (
    <>
      <button
        type="button"
        aria-label="Open Playbook"
        title="Playbook"
        aria-expanded={open}
        aria-controls="assistant-panel"
        className="playbook-ring fixed right-4 bottom-4 z-50 grid size-14 place-items-center rounded-full border-[3px] text-teal-700 shadow-lg transition-transform outline-none hover:scale-105 focus-visible:ring-3 focus-visible:ring-ring/50 dark:text-teal-300"
        onClick={() => setOpen((current) => !current)}
      >
        <RugbyBall aria-hidden="true" className="size-6" />
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
    </>
  )
}
