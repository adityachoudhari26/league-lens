import { LayoutDashboard, UsersRound, Swords, Trophy, Settings2 } from "lucide-react"

export const navigation = [
  { path: "/", title: "Overview", icon: LayoutDashboard, description: "A clearer view of your fantasy league.", emptyTitle: "Your league, in focus", emptyDescription: "A home for your team’s latest updates and the week ahead." },
  { path: "/roster", title: "Roster", icon: UsersRound, description: "Every player. One place.", emptyTitle: "Meet your roster", emptyDescription: "Your starters, bench, and reserves will live here." },
  { path: "/matchups", title: "Matchups", icon: Swords, description: "Follow the matchups around your league.", emptyTitle: "Every round has a story", emptyDescription: "Explore opponents, scores, and results across the season." },
  { path: "/standings", title: "Standings", icon: Trophy, description: "See where everyone stands.", emptyTitle: "The bigger picture", emptyDescription: "Team records and league standings will live here." },
  { path: "/settings", title: "Settings", icon: Settings2, description: "Make League Lens feel like yours.", emptyTitle: "", emptyDescription: "" },
] as const

export type NavigationItem = (typeof navigation)[number]
