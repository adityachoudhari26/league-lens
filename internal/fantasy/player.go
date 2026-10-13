package fantasy

type PlayerKind string

const (
	PlayerPerson      PlayerKind = "person"
	PlayerTeamDefense PlayerKind = "team_defense"
)

type Player struct {
	ExternalID string // ID in League.Ref.Provider's player namespace.
	Name       string // A provider-ID display fallback is allowed when unknown.
	Kind       PlayerKind

	// Only explicit, verified aliases supplied by the provider. Never infer
	// these from names. The shared identity resolver adds dataset mappings.
	KnownIDs []PlayerID
}

// PlayerNamespace is distinct from ProviderID: identity datasets also include
// namespaces such as GSIS that do not implement the league Provider interface.
type PlayerNamespace string

const (
	PlayerIDSleeper PlayerNamespace = "sleeper"
	PlayerIDESPN    PlayerNamespace = "espn"
	PlayerIDGSIS    PlayerNamespace = "gsis"
	PlayerIDMFL     PlayerNamespace = "mfl"
)

type PlayerID struct {
	Namespace  PlayerNamespace
	ExternalID string
}

type Position string

const (
	PositionQB  Position = "QB"
	PositionRB  Position = "RB"
	PositionWR  Position = "WR"
	PositionTE  Position = "TE"
	PositionK   Position = "K"
	PositionDEF Position = "DEF"
	PositionDL  Position = "DL"
	PositionLB  Position = "LB"
	PositionDB  Position = "DB"
	PositionDT  Position = "DT"
	PositionDE  Position = "DE"
	PositionCB  Position = "CB"
	PositionS   Position = "S"
)
