import { useState } from "react"
import type { ConnectedLeague } from "@/gen/leaguelens/v1/connections_pb"
import { observedTime } from "@/components/overview/format"
import { Button } from "@/components/ui/button"
import { useRemoveLeague } from "@/lib/api/connections"

export function ConnectedLeagueRow({ league }: { league: ConnectedLeague }) {
  const [confirming, setConfirming] = useState(false)
  const remove = useRemoveLeague()

  async function confirmRemove() {
    try {
      await remove.mutateAsync(league.leagueId)
      setConfirming(false)
    } catch {
      return
    }
  }

  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3 first:pt-0 last:pb-0">
      <p className="min-w-0 flex-1 text-sm">
        <span className="font-medium">{league.name}</span>
        <span className="text-muted-foreground">
          {" "}
          · {league.season} · Synced {observedTime(league.lastSuccessfulSyncAt)}
        </span>
      </p>
      {confirming ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">
            Remove from League Lens? Saved history is kept.
          </span>
          <Button
            size="sm"
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => void confirmRemove()}
          >
            {remove.isPending ? "Removing…" : "Remove"}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => {
              setConfirming(false)
              remove.reset()
            }}
          >
            Cancel
          </Button>
        </div>
      ) : (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            remove.reset()
            setConfirming(true)
          }}
        >
          Remove
        </Button>
      )}
      {remove.isError && (
        <p
          role="alert"
          className="basis-full text-xs text-amber-800 dark:text-[#e0c48f]"
        >
          Couldn’t remove this league. Try again.
        </p>
      )}
    </li>
  )
}
