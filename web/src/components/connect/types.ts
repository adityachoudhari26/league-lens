import type { DiscoveredLeague } from "@/gen/leaguelens/v1/connections_pb"

export type Provider = "espn" | "sleeper"

export type Step = "provider" | "credentials" | "leagues" | "results"

export type ESPNCredentials = { s2: string; swid: string }

export function leagueKey(league: DiscoveredLeague) {
  return `${league.externalLeagueId}:${league.teamId}`
}
