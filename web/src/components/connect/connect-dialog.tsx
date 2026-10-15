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
      <DialogContent className="max-h-[min(85vh,44rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <ProviderStep
          hidden={flow.step !== "provider"}
          onChoose={flow.chooseProvider}
        />
        <EspnStep
          hidden={flow.step !== "credentials" || flow.provider !== "espn"}
          initialCredentials={initialESPN}
          error={flow.error}
          pending={flow.espnPending}
          formResetVersion={flow.formResetVersion}
          espnClearVersion={flow.espnClearVersion}
          onBack={() => flow.goToStep("provider")}
          onSubmit={flow.submitESPN}
        />
        <SleeperStep
          hidden={flow.step !== "credentials" || flow.provider !== "sleeper"}
          error={flow.error}
          pending={flow.sleeperPending}
          formResetVersion={flow.formResetVersion}
          onBack={() => flow.goToStep("provider")}
          onSubmit={flow.submitSleeper}
        />
        <LeagueChoiceStep
          hidden={flow.step !== "leagues"}
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
        <ImportResultsStep
          hidden={flow.step !== "results"}
          results={flow.results}
          leagues={flow.leagues}
          onDone={() => {
            flow.reset()
            onOpenChange(false)
          }}
        />
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
