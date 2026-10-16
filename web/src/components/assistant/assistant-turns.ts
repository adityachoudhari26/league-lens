import { create } from "@bufbuild/protobuf"
import { Code, ConnectError } from "@connectrpc/connect"
import {
  ChatMessageSchema,
  ChatRole,
  type ActionProposal,
  type ChatMessage,
  type ChatResponse,
} from "@/gen/leaguelens/v1/assistant_pb"

export type AssistantTurn = {
  kind: "message"
  role: "user" | "assistant"
  text: string
  toolActivities?: string[]
  actionProposal?: ActionProposal
  actionChoice?: "confirmed" | "cancelled"
  actionPending?: boolean
}

export type ChatProblem = {
  requiresSetup: boolean
  message?: string
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
    return appendAssistantToolActivity(turns, response.event.value.label)
  }
  if (response.event.case === "proposal") {
    return appendAssistantProposal(turns, response.event.value)
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

function appendAssistantToolActivity(
  messages: AssistantTurn[],
  label: string,
): AssistantTurn[] {
  const last = messages[messages.length - 1]
  if (last?.kind === "message" && last.role === "assistant") {
    return [
      ...messages.slice(0, -1),
      { ...last, toolActivities: [...(last.toolActivities ?? []), label] },
    ]
  }
  return [
    ...messages,
    { kind: "message", role: "assistant", text: "", toolActivities: [label] },
  ]
}

function appendAssistantProposal(
  messages: AssistantTurn[],
  proposal: ActionProposal,
): AssistantTurn[] {
  const last = messages[messages.length - 1]
  if (last?.role === "assistant") {
    return [...messages.slice(0, -1), { ...last, actionProposal: proposal }]
  }
  return [
    ...messages,
    { kind: "message", role: "assistant", text: "", actionProposal: proposal },
  ]
}

export function updateTurn(
  messages: AssistantTurn[],
  index: number,
  patch: Partial<AssistantTurn>,
): AssistantTurn[] {
  return messages.map((turn, turnIndex) =>
    turnIndex === index ? { ...turn, ...patch } : turn,
  )
}

export function appendAssistantOutcome(
  messages: AssistantTurn[],
  text: string,
): AssistantTurn[] {
  return [...messages, { kind: "message", role: "assistant", text }]
}

export function chatErrorMessage(error: unknown): ChatProblem {
  if (error instanceof ConnectError) {
    if (error.code === Code.FailedPrecondition) {
      return { requiresSetup: true }
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
