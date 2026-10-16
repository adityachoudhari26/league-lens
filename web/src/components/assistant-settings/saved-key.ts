import {
  type AssistantProvider,
  type AssistantSettings,
} from "@/gen/leaguelens/v1/assistant_pb"

export function savedKeyMatches(
  savedSettings: AssistantSettings | undefined,
  provider: AssistantProvider,
  baseUrl: string,
): boolean {
  return (
    savedSettings !== undefined &&
    savedSettings.provider === provider &&
    savedSettings.baseUrl === baseUrl
  )
}
