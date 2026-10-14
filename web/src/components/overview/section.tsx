import { type ReactNode } from "react"
import { Button } from "@/components/ui/button"

export function Section({
  title,
  count,
  action,
  children,
}: {
  title: string
  count?: ReactNode
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section aria-label={title}>
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-base font-semibold tracking-tight">{title}</h2>
        {count !== undefined && (
          <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {count}
          </span>
        )}
        {action && <div className="ml-auto">{action}</div>}
      </div>
      {children}
    </section>
  )
}

export function SectionMessage({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-lg border bg-card px-3 py-2.5 text-sm leading-6 text-muted-foreground">
      {children}
    </div>
  )
}

export function SectionLoading() {
  return (
    <SectionMessage>
      <span role="status">Loading…</span>
    </SectionMessage>
  )
}

export function SectionError({ retry }: { retry: () => void }) {
  return (
    <SectionMessage>
      <p role="alert">
        Couldn’t load this section. Check that the API is running and try again.
      </p>
      <Button variant="outline" size="sm" className="mt-3" onClick={retry}>
        Try again
      </Button>
    </SectionMessage>
  )
}
