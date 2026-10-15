import { useCallback, useEffect, useRef, useState } from "react"
import type {
  DiscoveredLeague,
  ImportResult,
} from "@/gen/leaguelens/v1/connections_pb"
import {
  connectErrorMessage,
  useConnectESPN,
  useConnectSleeper,
  useImportLeagues,
} from "@/lib/api/connections"
import { leagueKey } from "./types"
import type { ESPNCredentials, Provider, Step } from "./types"

export function useConnectFlow({
  open,
  initialProvider,
  initialESPN,
}: {
  open: boolean
  initialProvider?: Provider
  initialESPN?: ESPNCredentials
}) {
  const [step, setStep] = useState<Step>(
    initialProvider ? "credentials" : "provider",
  )
  const [provider, setProvider] = useState<Provider | undefined>(
    initialProvider,
  )
  const [connectionId, setConnectionId] = useState("")
  const [leagues, setLeagues] = useState<DiscoveredLeague[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [results, setResults] = useState<ImportResult[]>([])
  const [error, setError] = useState("")
  const [username, setUsername] = useState("")
  const [espnPrefill, setEspnPrefill] = useState(initialESPN)
  const initialSubmitStarted = useRef(false)
  const {
    mutateAsync: connectESPN,
    reset: resetESPN,
    isPending: espnPending,
  } = useConnectESPN()
  const sleeperMutation = useConnectSleeper()
  const importMutation = useImportLeagues()

  function reset() {
    setStep("provider")
    setProvider(undefined)
    setConnectionId("")
    setLeagues([])
    setSelected(new Set())
    setResults([])
    setError("")
    setUsername("")
    setEspnPrefill(undefined)
    resetESPN()
    sleeperMutation.reset()
    importMutation.reset()
  }

  const showDiscoveredLeagues = useCallback(
    (id: string, discovered: DiscoveredLeague[]) => {
      setConnectionId(id)
      setLeagues(discovered)
      setSelected(new Set())
      setResults([])
      setError("")
      setStep("leagues")
    },
    [],
  )

  const submitESPN = useCallback(
    async (credentials: ESPNCredentials) => {
      setError("")
      try {
        const response = await connectESPN(credentials)
        showDiscoveredLeagues(response.connectionId, response.leagues)
        setEspnPrefill(undefined)
      } catch (cause) {
        setError(connectErrorMessage(cause))
      } finally {
        resetESPN()
      }
    },
    [connectESPN, resetESPN, showDiscoveredLeagues],
  )

  useEffect(() => {
    if (!open) {
      initialSubmitStarted.current = false
      return
    }
    if (!initialESPN || initialSubmitStarted.current) return
    initialSubmitStarted.current = true
    void submitESPN(initialESPN)
  }, [open, initialESPN, submitESPN])

  const submitSleeper = useCallback(
    async (username: string) => {
      setError("")
      try {
        const response = await sleeperMutation.mutateAsync(username.trim())
        showDiscoveredLeagues(response.connectionId, response.leagues)
      } catch (cause) {
        setError(connectErrorMessage(cause))
      }
    },
    [showDiscoveredLeagues, sleeperMutation],
  )

  const importSelectedLeagues = useCallback(async () => {
    const picks = leagues
      .filter(
        (league) => !league.alreadyImported && selected.has(leagueKey(league)),
      )
      .map((league) => ({
        externalLeagueId: league.externalLeagueId,
        teamId: league.teamId,
      }))
    if (!picks.length) return

    setError("")
    try {
      const response = await importMutation.mutateAsync({
        connectionId,
        picks,
      })
      setResults(response.results)
      setStep("results")
    } catch (cause) {
      setError(connectErrorMessage(cause))
    }
  }, [connectionId, importMutation, leagues, selected])

  const chooseProvider = useCallback((nextProvider: Provider) => {
    setProvider(nextProvider)
    setStep("credentials")
  }, [])

  const goToStep = useCallback((nextStep: Step) => {
    setStep(nextStep)
  }, [])

  const toggleLeague = useCallback(
    (league: DiscoveredLeague, checked: boolean) => {
      setSelected((current) => {
        const next = new Set(current)
        if (checked) next.add(leagueKey(league))
        else next.delete(leagueKey(league))
        return next
      })
    },
    [],
  )

  const selectedCount = leagues.filter(
    (league) => !league.alreadyImported && selected.has(leagueKey(league)),
  ).length
  const allAlreadyImported =
    leagues.length > 0 && leagues.every((league) => league.alreadyImported)
  const pending =
    espnPending || sleeperMutation.isPending || importMutation.isPending

  return {
    step,
    provider,
    error,
    pending,
    leagues,
    selected,
    results,
    chooseProvider,
    goToStep,
    submitESPN,
    submitSleeper,
    toggleLeague,
    importSelectedLeagues,
    selectedCount,
    allAlreadyImported,
    reset,
    espnPending,
    sleeperPending: sleeperMutation.isPending,
    importPending: importMutation.isPending,
    username,
    setUsername,
    espnPrefill,
  }
}
