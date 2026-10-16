import { create } from "@bufbuild/protobuf"
import { Code, ConnectError } from "@connectrpc/connect"
import {
  ChatMessageSchema,
  ChatRole,
  type ChatMessage,
  type ChatResponse,
} from "@/gen/leaguelens/v1/assistant_pb"

export type AssistantTurn =
  | { kind: "message"; role: "user" | "assistant"; text: string }
  | { kind: "tool"; text: string }

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
