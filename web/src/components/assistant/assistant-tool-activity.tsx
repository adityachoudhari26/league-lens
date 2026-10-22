import { ChevronDown } from "lucide-react"
import { useState } from "react"
import { cn } from "@/lib/utils"

type AssistantToolActivityProps = {
  activities: string[]
  isWorking: boolean
}

/**
 * Collapses a reply's tool calls into one line: the latest call while the
 * reply is still working, then a count once it's done. Clicking the line
 * lists every call in order.
 */
export function AssistantToolActivity({
  activities,
  isWorking,
}: AssistantToolActivityProps) {
  const [expanded, setExpanded] = useState(false)
  const latest = activities[activities.length - 1]
  const summary = isWorking ? latest : checkCountLabel(activities.length)
  return (
    <div className="flex flex-col gap-1.5">
      <button
        type="button"
        aria-expanded={expanded}
        className="group flex items-center gap-1.5 self-start rounded-sm text-xs text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
        onClick={() => setExpanded((current) => !current)}
      >
        <span
          key={summary}
          className="animate-in fade-in slide-in-from-bottom-1 duration-300"
        >
          {summary}
        </span>
        {isWorking ? <TypingDots /> : null}
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "size-3.5 transition-transform",
            expanded && "rotate-180",
          )}
        />
      </button>
      {expanded ? <ToolActivityList activities={activities} /> : null}
    </div>
  )
}

function ToolActivityList({ activities }: { activities: string[] }) {
  return (
    <ol className="ml-1 flex animate-in flex-col gap-1 border-l border-border pl-3 fade-in duration-200">
      {activities.map((activity, index) => (
        <li
          key={`${activity}-${index}`}
          className="text-xs text-muted-foreground"
        >
          {activity}
        </li>
      ))}
    </ol>
  )
}

/** Three dots that bounce in turn while Playbook is working. */
export function TypingDots({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn("inline-flex items-center gap-0.5", className)}
    >
      {[0, 150, 300].map((delay) => (
        <span
          key={delay}
          className="size-1 animate-typing-dot rounded-full bg-current motion-reduce:animate-none"
          style={{ animationDelay: `${delay}ms` }}
        />
      ))}
    </span>
  )
}

function checkCountLabel(count: number): string {
  return count === 1 ? "Ran 1 check" : `Ran ${count} checks`
}
