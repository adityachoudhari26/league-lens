import { useCallback, useEffect, useRef, useState } from "react"
import type {
  ChatRequest,
  ChatResponse,
} from "@/gen/leaguelens/v1/assistant_pb"
import { assistant } from "@/lib/api/client"

export function useChatStream() {
  const [isStreaming, setIsStreaming] = useState(false)
  const abortController = useRef<AbortController | undefined>(undefined)

  const stop = useCallback(() => abortController.current?.abort(), [])

  const stream = useCallback(
    async (
      request: ChatRequest,
      onResponse: (response: ChatResponse) => void,
    ): Promise<void> => {
      const controller = new AbortController()
      abortController.current = controller
      setIsStreaming(true)
      try {
        for await (const response of assistant.chat(request, {
          signal: controller.signal,
        })) {
          onResponse(response)
        }
      } catch (error) {
        if (!controller.signal.aborted) throw error
      } finally {
        if (abortController.current === controller) {
          abortController.current = undefined
        }
        setIsStreaming(false)
      }
    },
    [],
  )

  useEffect(() => () => abortController.current?.abort(), [])

  return { isStreaming, stream, stop }
}
