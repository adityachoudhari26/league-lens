import { useEffect } from "react"
import { type NavigationItem } from "@/components/layout/navigation"
import { LeagueSettings } from "@/components/connect/league-settings"
import { Button } from "@/components/ui/button"
import { themeOptions } from "@/components/theme/theme-toggle"
import { useTheme } from "@/components/theme/theme-provider"

export function PageHeading({ item }: { item: NavigationItem }) {
  return (
    <div className="mb-6">
      <h1 className="text-2xl font-semibold tracking-tight">{item.title}</h1>
      <p className="mt-1.5 text-sm text-muted-foreground">{item.description}</p>
    </div>
  )
}

function PagePlaceholder({ item }: { item: NavigationItem }) {
  return (
    <section
      aria-label={`${item.title} preview`}
      className="flex min-h-96 flex-1 items-center justify-center rounded-xl border border-border/80 bg-card p-8 sm:min-h-[440px]"
    >
      <div className="max-w-sm text-center">
        <div className="mx-auto mb-6 flex size-14 items-center justify-center rounded-2xl border bg-background shadow-xs">
          <item.icon
            className="size-6 text-muted-foreground"
            strokeWidth={1.5}
            aria-hidden="true"
          />
        </div>
        <h2 className="text-xl font-medium tracking-tight">
          {item.emptyTitle}
        </h2>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          {item.emptyDescription}
        </p>
        <span className="mt-6 inline-flex rounded-full border px-3 py-1 text-xs text-muted-foreground">
          Coming next
        </span>
      </div>
    </section>
  )
}

function AppearanceSettings() {
  const { theme, setTheme } = useTheme()
  return (
    <section
      className="max-w-2xl rounded-xl border bg-card p-6 sm:p-8"
      aria-labelledby="appearance-heading"
    >
      <h2 id="appearance-heading" className="text-base font-medium">
        Appearance
      </h2>
      <p className="mt-1.5 text-sm text-muted-foreground">
        Choose a theme, or follow your device. Your preference is saved in this
        browser.
      </p>
      <div
        className="mt-6 flex flex-wrap gap-3"
        role="group"
        aria-label="Color theme"
      >
        {themeOptions.map(({ value, label, icon: Icon }) => (
          <Button
            key={value}
            variant={theme === value ? "default" : "outline"}
            size="lg"
            aria-pressed={theme === value}
            onClick={() => setTheme(value)}
          >
            <Icon aria-hidden="true" />
            {label}
          </Button>
        ))}
      </div>
    </section>
  )
}

export function WorkspacePage({ item }: { item: NavigationItem }) {
  useEffect(() => {
    document.title = `${item.title} · League Lens`
  }, [item.title])
  return (
    <>
      <PageHeading item={item} />
      {item.path === "/settings" ? (
        <div className="grid gap-6">
          <LeagueSettings />
          <AppearanceSettings />
        </div>
      ) : (
        <PagePlaceholder item={item} />
      )}
    </>
  )
}
