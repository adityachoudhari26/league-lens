import { type ScoreTone } from "@/components/matchups/rules"

// Outcome colors for the owner's numbers, shared by the header and lineup.
// Light mode stays muted; dark mode is brighter to read on the dark card.
export const scoreToneClass: Record<ScoreTone, string> = {
  winning: "text-teal-700/80 dark:text-[#86d9b3]",
  losing: "text-red-700/80 dark:text-[#f09591]",
}
