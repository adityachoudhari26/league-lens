import type { CSSProperties } from "react"
import { Outlet, useRouterState } from "@tanstack/react-router"
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { Separator } from "@/components/ui/separator"
import { ThemeToggle } from "@/components/theme/theme-toggle"
import { AppSidebar } from "./app-sidebar"
import { navigation } from "./navigation"

function AppHeader() {
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const title =
    pathname === "/connect"
      ? "Connect leagues"
      : (navigation.find((item) => item.path === pathname)?.title ??
        "Page not found")
  return (
    <header className="flex h-16 shrink-0 items-center justify-between gap-4 border-b border-border px-4 lg:px-6">
      <div className="flex min-w-0 items-center gap-4">
        <SidebarTrigger />
        <Separator
          orientation="vertical"
          className="h-4! data-vertical:self-center"
        />
        <span className="truncate text-sm font-medium">{title}</span>
      </div>
      <ThemeToggle />
    </header>
  )
}

export function AppShell() {
  return (
    <SidebarProvider style={{ "--sidebar-width": "14rem" } as CSSProperties}>
      <a
        href="#main-content"
        className="sr-only z-50 rounded-md bg-primary p-3 text-primary-foreground focus:not-sr-only focus:absolute focus:left-4 focus:top-4"
      >
        Skip to content
      </a>
      <AppSidebar />
      <SidebarInset className="min-w-0">
        <AppHeader />
        <main
          id="main-content"
          tabIndex={-1}
          className="flex flex-1 flex-col px-4 py-6 outline-none sm:px-6"
        >
          <div className="flex w-full flex-1 flex-col">
            <Outlet />
          </div>
        </main>
      </SidebarInset>
    </SidebarProvider>
  )
}
