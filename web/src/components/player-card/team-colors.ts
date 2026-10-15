// Primary colors from ESPN's public NFL teams endpoint, keyed by the team
// abbreviations our adapters store. Pittsburgh and Las Vegas use their
// secondary color because their black primary would disappear on the card.
const TEAM_COLORS: Record<string, string> = {
  ARI: "#a40227",
  ATL: "#a71930",
  BAL: "#29126f",
  BUF: "#00338d",
  CAR: "#0085ca",
  CHI: "#0b1c3a",
  CIN: "#fb4f14",
  CLE: "#472a08",
  DAL: "#002a5c",
  DEN: "#0a2343",
  DET: "#0076b6",
  GB: "#204e32",
  HOU: "#021018",
  IND: "#003b75",
  JAX: "#007487",
  KC: "#e31837",
  LV: "#a5acaf",
  LAC: "#0080c6",
  LAR: "#003594",
  MIA: "#008e97",
  MIN: "#4f2683",
  NE: "#002a5c",
  NO: "#d3bc8d",
  NYG: "#003c7f",
  NYJ: "#115740",
  PHI: "#06424d",
  PIT: "#ffb612",
  SF: "#aa0000",
  SEA: "#002a5c",
  TB: "#bd1c36",
  TEN: "#4495d2",
  WAS: "#5a1414",
  WSH: "#5a1414",
}

export function teamColor(proTeam: string) {
  return TEAM_COLORS[proTeam.toUpperCase()]
}
