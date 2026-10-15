import { useCallback, useEffect, useRef, useState } from "react"
import { Check, CircleAlert } from "lucide-react"
import type {
  DiscoveredLeague,
  ImportResult,
} from "@/gen/leaguelens/v1/connections_pb"
import {
  connectErrorMessage,
  importErrorMessage,
  useConnectESPN,
  useConnectSleeper,
  useImportLeagues,
} from "@/lib/api/connections"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { EspnBookmarklet } from "./espn-bookmarklet"

type Provider = "espn" | "sleeper"
type Step = "provider" | "credentials" | "leagues" | "results"

export function ConnectDialog({
  open,
  onOpenChange,
  initialProvider,
  initialESPN,
  showSettingsHint = false,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialProvider?: Provider
  initialESPN?: { s2: string; swid: string }
  showSettingsHint?: boolean
}) {
  const [step, setStep] = useState<Step>(
    initialProvider ? "credentials" : "provider",
  )
  const [provider, setProvider] = useState<Provider | undefined>(
    initialProvider,
  )
  const [s2, setS2] = useState(initialESPN?.s2 ?? "")
  const [swid, setSwid] = useState(initialESPN?.swid ?? "")
  const [username, setUsername] = useState("")
  const [connectionId, setConnectionId] = useState("")
  const [leagues, setLeagues] = useState<DiscoveredLeague[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [results, setResults] = useState<ImportResult[]>([])
  const [error, setError] = useState("")
  const initialSubmitStarted = useRef(false)
  const {
    mutateAsync: connectESPN,
    reset: resetESPN,
    isPending: espnPending,
  } = useConnectESPN()
  const sleeperMutation = useConnectSleeper()
  const importMutation = useImportLeagues()

  function resetFlow() {
    setStep("provider")
    setProvider(undefined)
    setS2("")
    setSwid("")
    setUsername("")
    setConnectionId("")
    setLeagues([])
    setSelected(new Set())
    setResults([])
    setError("")
    resetESPN()
    sleeperMutation.reset()
    importMutation.reset()
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) resetFlow()
    onOpenChange(nextOpen)
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
    async (credentials: { s2: string; swid: string }) => {
      setError("")
      try {
        const response = await connectESPN(credentials)
        showDiscoveredLeagues(response.connectionId, response.leagues)
        setS2("")
        setSwid("")
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

  async function submitSleeper(value: string) {
    setError("")
    try {
      const response = await sleeperMutation.mutateAsync(value.trim())
      showDiscoveredLeagues(response.connectionId, response.leagues)
    } catch (cause) {
      setError(connectErrorMessage(cause))
    }
  }

  async function importSelectedLeagues() {
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
  }

  function toggleLeague(league: DiscoveredLeague, checked: boolean) {
    setSelected((current) => {
      const next = new Set(current)
      if (checked) next.add(leagueKey(league))
      else next.delete(leagueKey(league))
      return next
    })
  }

  const selectedCount = leagues.filter(
    (league) => !league.alreadyImported && selected.has(leagueKey(league)),
  ).length
  const allAlreadyImported =
    leagues.length > 0 && leagues.every((league) => league.alreadyImported)
  const pending =
    espnPending || sleeperMutation.isPending || importMutation.isPending
  const title =
    step === "provider"
      ? "Connect a league"
      : step === "credentials"
        ? provider === "espn"
          ? "Connect ESPN"
          : "Connect Sleeper"
        : step === "leagues"
          ? "Choose leagues"
          : "Import results"
  const description =
    step === "provider"
      ? "Choose the fantasy platform for your league."
      : step === "credentials"
        ? provider === "espn"
          ? "Use a bookmarklet or paste your ESPN cookies to find your leagues."
          : "Enter your Sleeper username to find your leagues."
        : step === "leagues"
          ? "Select the leagues and teams to add to League Lens."
          : "Here’s what happened with each selected league."

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[min(85vh,44rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        {step === "provider" && (
          <div className="grid gap-3">
            <ProviderButton
              provider="espn"
              onClick={() => {
                setProvider("espn")
                setStep("credentials")
              }}
            />
            <ProviderButton
              provider="sleeper"
              onClick={() => {
                setProvider("sleeper")
                setStep("credentials")
              }}
            />
          </div>
        )}

        {step === "credentials" && provider === "espn" && (
          <div className="grid gap-5">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-fit px-0 text-muted-foreground"
              onClick={() => setStep("provider")}
            >
              Back to platforms
            </Button>
            <EspnBookmarklet />
            <form
              className="grid gap-3"
              onSubmit={(event) => {
                event.preventDefault()
                void submitESPN({ s2, swid })
              }}
            >
              <div>
                <h3 className="text-sm font-medium">Or paste cookies</h3>
                <p className="mt-1 text-xs text-muted-foreground">
                  Cookies are sent securely to this local app and never saved in
                  your browser.
                </p>
              </div>
              <label className="grid gap-1.5 text-sm" htmlFor="espn-s2">
                espn_s2
                <Input
                  id="espn-s2"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  value={s2}
                  onChange={(event) => setS2(event.target.value)}
                  required
                />
              </label>
              <label className="grid gap-1.5 text-sm" htmlFor="espn-swid">
                SWID
                <Input
                  id="espn-swid"
                  type="text"
                  autoComplete="off"
                  spellCheck={false}
                  value={swid}
                  onChange={(event) => setSwid(event.target.value)}
                  required
                />
              </label>
              <p className="text-xs text-muted-foreground">
                DevTools → Application → Cookies → https://www.espn.com
              </p>
              {error && <InlineError>{error}</InlineError>}
              <Button
                type="submit"
                disabled={espnPending || !s2.trim() || !swid.trim()}
              >
                {espnPending ? "Connecting…" : "Find leagues"}
              </Button>
            </form>
          </div>
        )}

        {step === "credentials" && provider === "sleeper" && (
          <form
            className="grid gap-4"
            onSubmit={(event) => {
              event.preventDefault()
              void submitSleeper(username)
            }}
          >
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-fit px-0 text-muted-foreground"
              onClick={() => setStep("provider")}
            >
              Back to platforms
            </Button>
            <label className="grid gap-1.5 text-sm" htmlFor="sleeper-username">
              Sleeper username
              <Input
                id="sleeper-username"
                autoComplete="username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                required
              />
            </label>
            {error && <InlineError>{error}</InlineError>}
            <Button
              type="submit"
              disabled={sleeperMutation.isPending || !username.trim()}
            >
              {sleeperMutation.isPending ? "Finding leagues…" : "Find leagues"}
            </Button>
          </form>
        )}

        {step === "leagues" && (
          <div className="grid gap-4">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-fit px-0 text-muted-foreground"
              onClick={() => setStep("credentials")}
            >
              Back to credentials
            </Button>
            {leagues.length ? (
              <div className="grid gap-2">
                {leagues.map((league) => (
                  <LeagueChoice
                    key={leagueKey(league)}
                    league={league}
                    checked={
                      league.alreadyImported || selected.has(leagueKey(league))
                    }
                    onCheckedChange={(checked) => toggleLeague(league, checked)}
                  />
                ))}
              </div>
            ) : (
              <p className="rounded-lg border px-4 py-3 text-sm text-muted-foreground">
                No leagues were found for this account.
              </p>
            )}
            {error && <InlineError>{error}</InlineError>}
            {allAlreadyImported ? (
              <>
                <p className="text-sm text-muted-foreground">
                  All of these leagues are already in League Lens.
                </p>
                <Button type="button" onClick={() => handleOpenChange(false)}>
                  Done
                </Button>
              </>
            ) : (
              <Button
                type="button"
                onClick={() => void importSelectedLeagues()}
                disabled={!selectedCount || importMutation.isPending}
              >
                {importMutation.isPending
                  ? "Importing…"
                  : `Import ${selectedCount} ${selectedCount === 1 ? "league" : "leagues"}`}
              </Button>
            )}
          </div>
        )}

        {step === "results" && (
          <div className="grid gap-4">
            <div className="grid gap-2">
              {results.map((result) => {
                const league = leagues.find(
                  (candidate) =>
                    candidate.externalLeagueId === result.externalLeagueId &&
                    candidate.teamId === result.teamId,
                )
                return (
                  <ImportResultRow
                    key={`${result.externalLeagueId}:${result.teamId}`}
                    result={result}
                    name={league?.name ?? result.externalLeagueId}
                    season={league?.season}
                  />
                )
              })}
            </div>
            <Button type="button" onClick={() => handleOpenChange(false)}>
              Done
            </Button>
          </div>
        )}

        {error && step !== "credentials" && step !== "leagues" && (
          <InlineError>{error}</InlineError>
        )}
        {showSettingsHint && (
          <p className="border-t pt-3 text-xs text-muted-foreground">
            You can also connect leagues in Settings.
          </p>
        )}
        {pending && (
          <span className="sr-only" role="status">
            Working…
          </span>
        )}
      </DialogContent>
    </Dialog>
  )
}

function ProviderButton({
  provider,
  onClick,
}: {
  provider: Provider
  onClick: () => void
}) {
  const isEspn = provider === "espn"
  return (
    <Button
      type="button"
      variant="outline"
      className="h-auto w-full justify-start gap-4 whitespace-normal px-4 py-4 text-left"
      onClick={onClick}
    >
      <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted/50 text-base font-semibold">
        {isEspn ? "E" : "S"}
      </span>
      <span className="grid gap-1">
        <span className="font-medium">{isEspn ? "ESPN" : "Sleeper"}</span>
        <span className="text-xs font-normal text-muted-foreground">
          {isEspn
            ? "Find leagues with your ESPN login"
            : "Look up leagues by username"}
        </span>
      </span>
    </Button>
  )
}

function LeagueChoice({
  league,
  checked,
  onCheckedChange,
}: {
  league: DiscoveredLeague
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div
      className={`flex items-start gap-3 rounded-lg border px-3 py-3 ${
        league.alreadyImported
          ? "cursor-not-allowed bg-muted/30 text-muted-foreground"
          : "bg-card hover:bg-muted/30"
      }`}
    >
      <Checkbox
        aria-label={`${league.name}, ${league.season}, ${league.teamName || "your team"}`}
        checked={checked}
        disabled={league.alreadyImported}
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{league.name}</span>
          {league.alreadyImported && <ImportedBadge />}
        </span>
        <span className="mt-1 block text-xs text-muted-foreground">
          {league.season} · {league.teamName || "Your team"}
        </span>
      </span>
    </div>
  )
}

function ImportedBadge() {
  return (
    <span className="rounded-full border border-border bg-muted px-2 py-0.5 text-[0.7rem] text-muted-foreground">
      Imported
    </span>
  )
}

function ImportResultRow({
  result,
  name,
  season,
}: {
  result: ImportResult
  name: string
  season?: number
}) {
  return (
    <div className="flex items-start gap-3 rounded-lg border px-3 py-3">
      {result.succeeded ? (
        <Check className="mt-0.5 size-4 shrink-0 text-emerald-700 dark:text-[#9bc8bf]" />
      ) : (
        <CircleAlert className="mt-0.5 size-4 shrink-0 text-amber-700 dark:text-[#d8b77b]" />
      )}
      <div className="min-w-0">
        <p className="text-sm font-medium">
          {name}
          {season ? ` · ${season}` : ""}
        </p>
        {result.succeeded ? (
          <p className="mt-1 text-xs text-muted-foreground">Imported</p>
        ) : (
          <p
            role="alert"
            className="mt-1 text-sm text-amber-800 dark:text-[#e0c48f]"
          >
            {importErrorMessage(result.errorCode)}
          </p>
        )}
      </div>
    </div>
  )
}

function InlineError({ children }: { children: string }) {
  return (
    <p
      role="alert"
      className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-[#5a4a2e] dark:bg-[#332b1f] dark:text-[#e0c48f]"
    >
      {children}
    </p>
  )
}

function leagueKey(league: DiscoveredLeague) {
  return `${league.externalLeagueId}:${league.teamId}`
}
