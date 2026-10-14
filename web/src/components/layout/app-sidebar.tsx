import { Link, useRouterState } from "@tanstack/react-router"
import { Aperture } from "lucide-react"
import { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarHeader, SidebarMenu, SidebarMenuButton, SidebarMenuItem, useSidebar } from "@/components/ui/sidebar"
import { navigation, type NavigationItem } from "./navigation"

function NavigationLink({ item }: { item: NavigationItem }) {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const { setOpenMobile } = useSidebar()
  const active = pathname === item.path
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={active}
        tooltip={item.title}
        className="h-9 gap-3 px-3"
        render={<Link to={item.path} aria-current={active ? "page" : undefined} onClick={() => setOpenMobile(false)} />}
      >
        <item.icon aria-hidden="true" /><span>{item.title}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

export function AppSidebar() {
  return (
    <Sidebar collapsible="icon" className="border-border">
      <SidebarHeader className="h-16 shrink-0 justify-center border-b border-border px-3 group-data-[collapsible=icon]:px-2">
        <SidebarMenu><SidebarMenuItem>
          <SidebarMenuButton size="lg" className="gap-3 hover:bg-transparent" render={<Link to="/" aria-label="League Lens home" />}>
            <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground"><Aperture className="size-5!" aria-hidden="true" /></span>
            <span className="flex flex-col gap-0.5"><span className="text-base font-semibold tracking-tight">League Lens</span></span>
          </SidebarMenuButton>
        </SidebarMenuItem></SidebarMenu>
      </SidebarHeader>
      <SidebarContent className="px-2 pt-3 group-data-[collapsible=icon]:px-0">
        <SidebarGroup>
          <SidebarGroupContent><nav aria-label="Main navigation"><SidebarMenu className="gap-1">{navigation.slice(0, 4).map((item) => <NavigationLink key={item.path} item={item} />)}</SidebarMenu></nav></SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="gap-4 px-4 pb-3 group-data-[collapsible=icon]:px-2">
        <SidebarMenu><NavigationLink item={navigation[4]} /></SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}
