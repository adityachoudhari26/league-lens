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
const pages = navigation.map((item) =>
  createRoute({
    getParentRoute: () => rootRoute,
    path: item.path,
    component: () =>
      item.path === "/" ? (
        <OverviewPage />
      ) : item.path === "/matchups" ? (
        <MatchupsPage item={item} />
      ) : (
        <WorkspacePage item={item} />
      ),
  }),
)
export const router = createRouter({
  routeTree: rootRoute.addChildren(pages),
  scrollRestoration: true,
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}
