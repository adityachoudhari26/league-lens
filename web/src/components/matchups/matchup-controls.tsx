import { ChevronLeft, ChevronRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { type League } from "@/gen/leaguelens/v1/leagues_pb"

function ProviderBadge({ provider }: { provider: string }) {
  const espn = provider === "espn"
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${espn ? "bg-rose-50 text-rose-700 dark:bg-[#382c33] dark:text-[#d3a4af]" : "bg-violet-50 text-violet-700 dark:bg-[#322e3e] dark:text-[#bfb0d8]"}`}
    >
      {provider.toUpperCase()}
    </span>
  )
}

export function LeagueSelector({
  leagues,
  selected,
  onSelect,
}: {
  leagues: League[]
  selected: string
  onSelect: (leagueId: string) => void
}) {
  const items = leagues.map((league) => ({
    value: league.id,
    label: league.name,
  }))
  return (
    <Select
      items={items}
      value={selected}
      onValueChange={(value) => {
        if (value) onSelect(value)
      }}
    >
      <SelectTrigger aria-label="League" className="w-72">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {leagues.map((league) => (
          <SelectItem key={league.id} value={league.id}>
            <span className="truncate">{league.name}</span>
            <ProviderBadge provider={league.provider} />
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function WeekSelector({
  periods,
  period,
  onSelect,
}: {
  periods: number[]
  period?: number
  onSelect: (period: number) => void
}) {
  const index = period === undefined ? -1 : periods.indexOf(period)
  return (
    <div className="flex items-center gap-1">
      <Button
        variant="outline"
        size="icon-sm"
        aria-label="Previous week"
        disabled={index <= 0}
        onClick={() => onSelect(periods[index - 1])}
      >
        <ChevronLeft />
      </Button>
      <span className="w-16 text-center text-sm font-medium">
        Week {period ?? "—"}
      </span>
      <Button
        variant="outline"
        size="icon-sm"
        aria-label="Next week"
        disabled={index >= periods.length - 1}
        onClick={() => onSelect(periods[index + 1])}
      >
        <ChevronRight />
      </Button>
    </div>
  )
}
