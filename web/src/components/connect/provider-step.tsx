import { Button } from "@/components/ui/button"
import type { Provider } from "./types"

export function ProviderStep({
  onChoose,
}: {
  onChoose: (provider: Provider) => void
}) {
  return (
    <div className="grid gap-3">
      <ProviderButton provider="espn" onClick={() => onChoose("espn")} />
      <ProviderButton provider="sleeper" onClick={() => onChoose("sleeper")} />
    </div>
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
