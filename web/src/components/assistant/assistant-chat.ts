import { create } from "@bufbuild/protobuf"
import { useCallback, useEffect, useRef, useState } from "react"
import {
  ChatRequestSchema,
  type PageContext,
} from "@/gen/leaguelens/v1/assistant_pb"
import {
  fetchAssistantConversation,
  useRunAssistantAction,
} from "@/lib/api/assistant"
import {
  appendAssistantOutcome,
  applyChatResponse,
  chatErrorMessage,
  turnsFromConversation,
  type AssistantTurn,
  type ChatProblem,
  updateTurn,
} from "./assistant-turns"
import { useChatStream } from "./use-chat-stream"

const conversationStorageKey = "playbook-conversation-id"

function readStoredConversationId(): string | undefined {
  try {
    return localStorage.getItem(conversationStorageKey) || undefined
  } catch {
    return undefined
  }
}

function storeConversationId(conversationId: string) {
  try {
    localStorage.setItem(conversationStorageKey, conversationId)
  } catch {
    // Keep the current session usable without storage.
  }
}

function forgetStoredConversationId() {
  try {
    localStorage.removeItem(conversationStorageKey)
  } catch {
    // Keep the current session usable without storage.
  }
}

export function useAssistantChat(page: PageContext) {
  const { isStreaming, stream, stop } = useChatStream()
  const runAssistantAction = useRunAssistantAction()
  const [messages, setMessages] = useState<AssistantTurn[]>([])
  const [draft, setDraft] = useState("")
  const [problem, setProblem] = useState<ChatProblem>()
  const [isRestoring, setIsRestoring] = useState(
    () => readStoredConversationId() !== undefined,
  )
  const conversationId = useRef<string | undefined>(undefined)
  const handledActions = useRef(new Set<number>())
  const restoreController = useRef<AbortController | undefined>(undefined)

  useEffect(() => {
    const storedConversationId = readStoredConversationId()
    if (!storedConversationId) return

    const controller = new AbortController()
    restoreController.current = controller

    async function restoreConversation(id: string) {
      try {
        const response = await fetchAssistantConversation(id, controller.signal)
        if (controller.signal.aborted) return
        conversationId.current = id
        setMessages((current) =>
          current.length ? current : turnsFromConversation(response),
        )
      } catch {
        if (controller.signal.aborted) return
        conversationId.current = undefined
        forgetStoredConversationId()
      } finally {
        if (!controller.signal.aborted) {
          setIsRestoring(false)
          if (restoreController.current === controller) {
            restoreController.current = undefined
          }
        }
      }
    }

    void restoreConversation(storedConversationId)
    return () => {
      controller.abort()
      if (restoreController.current === controller) {
        restoreController.current = undefined
      }
    }
  }, [])

  const clear = useCallback(() => {
    stop()
    restoreController.current?.abort()
    restoreController.current = undefined
    handledActions.current.clear()
    conversationId.current = undefined
    forgetStoredConversationId()
    setMessages([])
    setDraft("")
    setProblem(undefined)
    setIsRestoring(false)
  }, [stop])

  const send = useCallback(
    async (value = draft) => {
      const text = value.trim()
      if (!text || isStreaming || isRestoring) return
      const nextMessages: AssistantTurn[] = [
        ...messages,
        { kind: "message", role: "user", text },
      ]
      setMessages(nextMessages)
      setDraft("")
      setProblem(undefined)
      try {
        const request = create(ChatRequestSchema, {
          conversationId: conversationId.current ?? "",
          message: text,
          page,
        })
        await stream(request, (event) => {
          if (event.event.case === "conversation") {
            const id = event.event.value.id
            conversationId.current = id
            storeConversationId(id)
            return
          }
          setMessages((current) => applyChatResponse(current, event))
        })
      } catch (cause) {
        setProblem(chatErrorMessage(cause))
      }
    },
    [draft, isRestoring, isStreaming, messages, page, stream],
  )

  const runAction = runAssistantAction.mutateAsync
  const confirmAction = useCallback(
    async (index: number) => {
      const turn = messages[index]
      const action = turn?.actionProposal?.action
      if (!action || handledActions.current.has(index)) return
      handledActions.current.add(index)
      setMessages((current) =>
        updateTurn(current, index, {
          actionChoice: "confirmed",
          actionPending: true,
        }),
      )
      try {
        const response = await runAction({
          action,
          conversationId: conversationId.current,
        })
        setMessages((current) =>
          appendAssistantOutcome(current, response.summary),
        )
      } catch {
        setMessages((current) =>
          appendAssistantOutcome(current, "Couldn't complete that action."),
        )
      } finally {
        setMessages((current) =>
          updateTurn(current, index, { actionPending: false }),
        )
      }
    },
    [messages, runAction],
  )

  const cancelAction = useCallback((index: number) => {
    if (handledActions.current.has(index)) return
    handledActions.current.add(index)
    setMessages((current) => {
      const decided = updateTurn(current, index, { actionChoice: "cancelled" })
      return appendAssistantOutcome(decided, "Cancelled.")
    })
  }, [])

  const isActionPending = messages.some((turn) => turn.actionPending)

  return {
    messages,
    draft,
    setDraft,
    error: problem?.message,
    requiresSetup: problem?.requiresSetup ?? false,
    isStreaming,
    isRestoring,
    isActionPending,
    hasPendingAction: messages.some(
      (turn) => turn.actionProposal && !turn.actionChoice,
    ),
    send,
    stop,
    clear,
    confirmAction,
    cancelAction,
  }
}
