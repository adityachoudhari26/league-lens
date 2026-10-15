import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { EspnStep } from "./espn-step"
import { ImportResultsStep } from "./import-results-step"
import { InlineError } from "./inline-error"
import { LeagueChoiceStep } from "./league-choice-step"
import { ProviderStep } from "./provider-step"
import { SleeperStep } from "./sleeper-step"
import { useConnectFlow } from "./use-connect-flow"
import type { ESPNCredentials, Provider, Step } from "./types"

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
  initialESPN?: ESPNCredentials
  showSettingsHint?: boolean
}) {
  const flow = useConnectFlow({ open, initialProvider, initialESPN })
  const { title, description } = stepCopy(flow.step, flow.provider)

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) flow.reset()
        onOpenChange(nextOpen)
      }}
    >
      <DialogContent className="max-h-[min(85vh,44rem)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="-mx-4 -mb-4 grid min-h-0 content-start gap-4 overflow-y-auto px-4 pb-4 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
          {flow.step === "provider" && (
            <ProviderStep onChoose={flow.chooseProvider} />
          )}
          {flow.step === "credentials" && flow.provider === "espn" && (
            <EspnStep
              initialCredentials={flow.espnPrefill}
              error={flow.error}
              pending={flow.espnPending}
              onBack={() => flow.goToStep("provider")}
              onSubmit={flow.submitESPN}
            />
          )}
          {flow.step === "credentials" && flow.provider === "sleeper" && (
            <SleeperStep
              username={flow.username}
              onUsernameChange={flow.setUsername}
              error={flow.error}
              pending={flow.sleeperPending}
              onBack={() => flow.goToStep("provider")}
              onSubmit={flow.submitSleeper}
            />
          )}
          {flow.step === "leagues" && (
            <LeagueChoiceStep
              leagues={flow.leagues}
              selected={flow.selected}
              selectedCount={flow.selectedCount}
              allAlreadyImported={flow.allAlreadyImported}
              error={flow.error}
              pending={flow.importPending}
              onBack={() => flow.goToStep("credentials")}
              onToggle={flow.toggleLeague}
              onImport={() => void flow.importSelectedLeagues()}
              onDone={() => {
                flow.reset()
                onOpenChange(false)
              }}
            />
          )}
          {flow.step === "results" && (
            <ImportResultsStep
              results={flow.results}
              leagues={flow.leagues}
              onDone={() => {
                flow.reset()
                onOpenChange(false)
              }}
            />
          )}
          {flow.error &&
            flow.step !== "credentials" &&
            flow.step !== "leagues" && <InlineError>{flow.error}</InlineError>}
          {showSettingsHint && (
            <p className="border-t pt-3 text-xs text-muted-foreground">
              You can also connect leagues in Settings.
            </p>
          )}
          {flow.pending && (
            <span className="sr-only" role="status">
              Working…
            </span>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function stepCopy(step: Step, provider?: Provider) {
  if (step === "provider") {
    return {
      title: "Connect a league",
      description: "Choose the fantasy platform for your league.",
    }
  }
  if (step === "credentials") {
    return provider === "espn"
      ? {
          title: "Connect ESPN",
          description:
            "Use the one-time bookmark (recommended) or paste two cookies.",
        }
      : {
          title: "Connect Sleeper",
          description: "Enter your Sleeper username to find your leagues.",
        }
  }
  if (step === "leagues") {
    return {
      title: "Choose leagues",
      description: "Select the leagues and teams to add to League Lens.",
    }
  }
  return {
    title: "Import results",
    description: "Here’s what happened with each selected league.",
  }
}
