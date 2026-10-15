import { useState } from "react"
import type { Provider } from "./types"

export function useConnectDialog() {
  const [open, setOpen] = useState(false)
  const [initialProvider, setInitialProvider] = useState<Provider>()

  function openConnect(provider?: Provider) {
    setInitialProvider(provider)
    setOpen(true)
  }

  function onOpenChange(nextOpen: boolean) {
    setOpen(nextOpen)
    if (!nextOpen) setInitialProvider(undefined)
  }

  return { open, initialProvider, openConnect, onOpenChange }
}
