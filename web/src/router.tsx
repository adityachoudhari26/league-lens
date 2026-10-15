import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
} from "@tanstack/react-router"
import { AppShell } from "@/components/layout/app-shell"
import { navigation } from "@/components/layout/navigation"
import { MatchupsPage } from "@/pages/matchups-page"
import { OverviewPage } from "@/pages/overview-page"
import { WorkspacePage } from "@/pages/workspace-page"

const rootRoute = createRootRoute({
  component: AppShell,
  notFoundComponent: () => (
    <div className="py-12">
      <h1 className="text-2xl font-semibold">Page not found</h1>
      <Link
        to="/"
        className="mt-4 inline-block text-sm underline underline-offset-4"
      >
        Back to overview
      </Link>
    </div>
  ),
})

const matchupsItem = navigation.find((item) => item.path === "/matchups")!

// Parses ?league=<id>&week=<n>: league is kept only when non-empty, week only
// when a positive integer (numeric strings accepted).
function validateMatchupsSearch(search: Record<string, unknown>): {
  league?: string
  week?: number
} {
  const league =
    typeof search.league === "string" && search.league !== ""
      ? search.league
      : undefined
  const raw = search.week
  const week =
    typeof raw === "number" ? raw : typeof raw === "string" ? Number(raw) : NaN
  return {
    league,
    week: Number.isInteger(week) && week > 0 ? week : undefined,
  }
}

const matchupsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: matchupsItem.path,
  component: () => <MatchupsPage item={matchupsItem} />,
  validateSearch: validateMatchupsSearch,
})

const pages = navigation
  .filter((item) => item.path !== matchupsItem.path)
  .map((item) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path: item.path,
      component: () =>
        item.path === "/" ? <OverviewPage /> : <WorkspacePage item={item} />,
    }),
  )

export const router = createRouter({
  routeTree: rootRoute.addChildren([...pages, matchupsRoute]),
  scrollRestoration: true,
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}
