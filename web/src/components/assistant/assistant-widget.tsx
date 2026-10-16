import { useState } from "react"
import { useRouterState } from "@tanstack/react-router"
import { MessageCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
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
      <Button
        type="button"
        size="icon"
        aria-label="Open assistant"
        aria-expanded={open}
        aria-controls="assistant-panel"
        className="fixed right-4 bottom-4 z-50 size-12 rounded-full shadow-md"
        onClick={() => setOpen((current) => !current)}
      >
        <MessageCircle aria-hidden="true" className="size-5" />
      </Button>
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
