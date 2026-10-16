import { create } from "@bufbuild/protobuf"
import { Code, ConnectError } from "@connectrpc/connect"
import { useCallback, useEffect, useRef, useState } from "react"
import {
  ChatMessageSchema,
  ChatRequestSchema,
  ChatRole,
  type ChatMessage,
  type ChatResponse,
  type PageContext,
} from "@/gen/leaguelens/v1/assistant_pb"
import { assistant } from "@/lib/api/client"

export type AssistantTurn =
  | { kind: "message"; role: "user" | "assistant"; text: string }
  | { kind: "tool"; text: string }

export function useAssistantChat(page: PageContext) {
  const [messages, setMessages] = useState<AssistantTurn[]>([])
  const [draft, setDraft] = useState("")
  const [error, setError] = useState<string>()
  const [requiresSetup, setRequiresSetup] = useState(false)
  const [isStreaming, setIsStreaming] = useState(false)
  const abortController = useRef<AbortController | undefined>(undefined)

  const stop = useCallback(() => abortController.current?.abort(), [])

  const clear = useCallback(() => {
    stop()
    setMessages([])
    setDraft("")
    setError(undefined)
    setRequiresSetup(false)
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
      setError(undefined)
      setRequiresSetup(false)
      setIsStreaming(true)
      const controller = new AbortController()
      abortController.current = controller
      try {
        const request = create(ChatRequestSchema, {
          messages: toChatMessages(nextMessages),
          page,
        })
        for await (const event of assistant.chat(request, {
          signal: controller.signal,
        })) {
          setMessages((current) => applyChatResponse(current, event))
        }
      } catch (chatError) {
        if (!controller.signal.aborted) {
          const result = chatErrorMessage(chatError)
          setRequiresSetup(result.requiresSetup)
          setError(result.message)
        }
      } finally {
        if (abortController.current === controller) {
          abortController.current = undefined
        }
        setIsStreaming(false)
      }
    },
    [draft, isStreaming, messages, page],
  )

  useEffect(() => () => abortController.current?.abort(), [])

  return {
    messages,
    draft,
    setDraft,
    error,
    requiresSetup,
    isStreaming,
    send,
    stop,
    clear,
  }
}

export function toChatMessages(turns: AssistantTurn[]): ChatMessage[] {
  return turns
    .filter(
      (turn): turn is Extract<AssistantTurn, { kind: "message" }> =>
        turn.kind === "message",
    )
    .slice(-40)
    .map((turn) =>
      create(ChatMessageSchema, {
        role: turn.role === "user" ? ChatRole.USER : ChatRole.ASSISTANT,
        text: turn.text,
      }),
    )
}

export function applyChatResponse(
  turns: AssistantTurn[],
  response: ChatResponse,
): AssistantTurn[] {
  if (response.event.case === "text") {
    return appendAssistantText(turns, response.event.value.text)
  }
  if (response.event.case === "tool") {
    return [...turns, { kind: "tool", text: response.event.value.label }]
  }
  return turns
}

function appendAssistantText(
  messages: AssistantTurn[],
  delta: string,
): AssistantTurn[] {
  if (!delta) return messages
  const last = messages[messages.length - 1]
  if (last?.kind === "message" && last.role === "assistant") {
    return [...messages.slice(0, -1), { ...last, text: last.text + delta }]
  }
  return [...messages, { kind: "message", role: "assistant", text: delta }]
}

function chatErrorMessage(error: unknown) {
  if (error instanceof ConnectError) {
    if (error.code === Code.FailedPrecondition) {
      return { requiresSetup: true, message: undefined }
    }
    if (error.code === Code.PermissionDenied) {
      return {
        requiresSetup: false,
        message: "Your AI key was rejected. Check it in Settings.",
      }
    }
    if (error.code === Code.Unavailable) {
      return {
        requiresSetup: false,
        message: "Couldn't reach the AI provider. Try again.",
      }
    }
  }
  return { requiresSetup: false, message: "Something went wrong. Try again." }
}
