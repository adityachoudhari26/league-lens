import { create } from "@bufbuild/protobuf"
import { useCallback, useRef, useState } from "react"
import {
  ChatRequestSchema,
  type PageContext,
} from "@/gen/leaguelens/v1/assistant_pb"
import { useRunAssistantAction } from "@/lib/api/assistant"
import {
  appendAssistantOutcome,
  applyChatResponse,
  chatErrorMessage,
  markActionChoice,
  markActionPending,
  toChatMessages,
  type AssistantTurn,
  type ChatProblem,
} from "./assistant-turns"
import { useChatStream } from "./use-chat-stream"

export function useAssistantChat(page: PageContext) {
  const { isStreaming, stream, stop } = useChatStream()
  const runAssistantAction = useRunAssistantAction()
  const [messages, setMessages] = useState<AssistantTurn[]>([])
  const [draft, setDraft] = useState("")
  const [problem, setProblem] = useState<ChatProblem>()
  const [isActionPending, setIsActionPending] = useState(false)
  const handledActions = useRef(new Set<number>())

  const clear = useCallback(() => {
    stop()
    handledActions.current.clear()
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

  const runAction = runAssistantAction.mutateAsync
  const confirmAction = useCallback(
    async (index: number) => {
      const turn = messages[index]
      const action = turn?.actionProposal?.action
      if (!action || handledActions.current.has(index)) return
      handledActions.current.add(index)
      setMessages((current) =>
        markActionChoice(current, index, "confirmed", true),
      )
      setIsActionPending(true)
      try {
        const response = await runAction(action)
        setMessages((current) =>
          appendAssistantOutcome(current, response.summary),
        )
      } catch {
        setMessages((current) =>
          appendAssistantOutcome(current, "Couldn't complete that action."),
        )
      } finally {
        setIsActionPending(false)
        setMessages((current) => markActionPending(current, index, false))
      }
    },
    [messages, runAction],
  )

  const cancelAction = useCallback((index: number) => {
    if (handledActions.current.has(index)) return
    handledActions.current.add(index)
    setMessages((current) => {
      const decided = markActionChoice(current, index, "cancelled", false)
      return appendAssistantOutcome(decided, "Cancelled.")
    })
  }, [])

  return {
    messages,
    draft,
    setDraft,
    error: problem?.message,
    requiresSetup: problem?.requiresSetup ?? false,
    isStreaming,
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
