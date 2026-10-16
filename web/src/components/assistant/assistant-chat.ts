import { create } from "@bufbuild/protobuf"
import { useCallback, useState } from "react"
import {
  ChatRequestSchema,
  type PageContext,
} from "@/gen/leaguelens/v1/assistant_pb"
import {
  applyChatResponse,
  chatErrorMessage,
  toChatMessages,
  type AssistantTurn,
  type ChatProblem,
} from "./assistant-turns"
import { useChatStream } from "./use-chat-stream"

export function useAssistantChat(page: PageContext) {
  const { isStreaming, stream, stop } = useChatStream()
  const [messages, setMessages] = useState<AssistantTurn[]>([])
  const [draft, setDraft] = useState("")
  const [problem, setProblem] = useState<ChatProblem>()

  const clear = useCallback(() => {
    stop()
    setMessages([])
    setDraft("")
    setProblem(undefined)
  }, [stop])

  const send = useCallback(
    async (value = draft) => {
      const text = value.trim()
      if (!text || isStreaming) return
      const nextMessages: AssistantTurn[] = [
        ...messages,
        { kind: "message", role: "user", text },
      ]
      setMessages(nextMessages)
      setDraft("")
      setProblem(undefined)
      try {
        const request = create(ChatRequestSchema, {
          messages: toChatMessages(nextMessages),
          page,
        })
        await stream(request, (event) =>
          setMessages((current) => applyChatResponse(current, event)),
        )
      } catch (cause) {
        setProblem(chatErrorMessage(cause))
      }
    },
    [draft, isStreaming, messages, page, stream],
  )

  return {
    messages,
    draft,
    setDraft,
    error: problem?.message,
    requiresSetup: problem?.requiresSetup ?? false,
    isStreaming,
    send,
    stop,
    clear,
  }
}
