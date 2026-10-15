import type { DiscoveredLeague } from "@/gen/leaguelens/v1/connections_pb"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { BackButton } from "./back-button"
import { ImportedBadge } from "./imported-badge"
import { InlineError } from "./inline-error"
import { leagueKey } from "./types"

export function LeagueChoiceStep({
  leagues,
  selected,
  selectedCount,
  allAlreadyImported,
  error,
  pending,
  onBack,
  onToggle,
  onImport,
  onDone,
}: {
  leagues: DiscoveredLeague[]
  selected: Set<string>
  selectedCount: number
  allAlreadyImported: boolean
  error: string
  pending: boolean
  onBack: () => void
  onToggle: (league: DiscoveredLeague, checked: boolean) => void
  onImport: () => void
  onDone: () => void
}) {
  return (
    <div className="grid gap-4">
      <BackButton onClick={onBack}>Back to credentials</BackButton>
      {leagues.length ? (
        <div className="grid gap-2">
          {leagues.map((league) => (
            <LeagueChoice
              key={leagueKey(league)}
              league={league}
              checked={
                league.alreadyImported || selected.has(leagueKey(league))
              }
              onCheckedChange={(checked) => onToggle(league, checked)}
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
          <Button type="button" onClick={onDone}>
            Done
          </Button>
        </>
      ) : (
        <Button
          type="button"
          onClick={onImport}
          disabled={!selectedCount || pending}
        >
          {pending
            ? "Importing…"
            : `Import ${selectedCount} ${selectedCount === 1 ? "league" : "leagues"}`}
        </Button>
      )}
    </div>
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
