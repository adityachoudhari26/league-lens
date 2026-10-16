import { Code, ConnectError } from "@connectrpc/connect"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { assistant } from "./client"

export const assistantQueries = {
  settings: {
    queryKey: ["assistant-settings"],
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      assistant.getAssistantSettings({}, { signal }),
  },
}

export function useAssistantSettings() {
  return useQuery(assistantQueries.settings)
}

export function useListAssistantModels() {
  return useMutation({
    mutationFn: (
      request: Parameters<typeof assistant.listAssistantModels>[0],
    ) => assistant.listAssistantModels(request),
  })
}

export function useSaveAssistantSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (
      request: Parameters<typeof assistant.saveAssistantSettings>[0],
    ) => assistant.saveAssistantSettings(request),
    onSuccess: async () =>
      queryClient.invalidateQueries({
        queryKey: assistantQueries.settings.queryKey,
      }),
  })
}

export function useDeleteAssistantSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => assistant.deleteAssistantSettings({}),
    onSuccess: async () =>
      queryClient.invalidateQueries({
        queryKey: assistantQueries.settings.queryKey,
      }),
  })
}

export function assistantErrorMessage(error: unknown) {
  if (!(error instanceof ConnectError)) {
    return "Assistant settings couldn’t be updated."
  }
  if (error.code === Code.PermissionDenied) {
    return "The provider rejected this key."
  }
  if (error.code === Code.Unavailable) {
    return "Couldn't reach the provider."
  }
  return "Assistant settings couldn’t be updated."
}
