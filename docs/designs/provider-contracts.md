# Provider import contracts

The provider boundary is `internal/fantasy.Provider`. It describes a league and
fetches a provider-independent payload. Adapter constructors receive credentials,
HTTP configuration, and provider-specific season selection. The importer receives
normalized data and owns transactions, sync status, deduplication, and player
identity resolution. Connection/team selection is application state, not provider
ownership metadata or authentication.

The contracts are ordinary Go types, written by hand. They are not Connect/Protobuf
API definitions, generated database bindings, or a plugin framework. Sleeper now implements this interface. Its normalization
helpers translate API responses into these types; the importer depends only on
`fantasy.Provider`. Username lookup lives in Sleeper connection-selection logic.
The existing import command and arguments remain available.

## Data semantics

- League identity includes provider, opaque string ID, and season.
- Fetch returns all league teams; required fetch failures return errors.
- Only encountered players are included. Provider-supplied aliases and shared
  mapping-dataset resolution are separate responsibilities.
- Adapters normalize roster entries and positions before persistence. Unknown
  lineup slots retain their original ID and use `SlotUnknown`.
- Roster history specifies the scoring week, origin, coverage, observation time,
  and any known effective time. Unavailable history is omitted. Full, empty
  rosters differ from missing rosters and lineup-only snapshots.
- Matchup periods can span several scoring weeks. Team selection, weekly rosters,
  and matchup pairings must never be conflated.
- Nullable scores distinguish actual zero from unavailable points. A missing
  opponent is not automatically a bye. Future schedules are representable but
  remain outside the current import scope.
- Only explicitly complete matchup periods authorize removing stale pairings.
  Omitted periods preserve prior evidence; omissions alone are never deletions.
- Normalize common flat-rate scoring rules and retain supplemental settings as
  credential-free JSON. Unsupported bonuses must not masquerade as complete
  normalized scoring; a complete scoring engine is not part of this checkpoint.

These contracts describe the import boundary rather than mirroring the current
schema. Migration `00002_provider_contract_fields.sql` retains team records/ranks,
matchup scoring-week spans, explicit byes, and nullable roster eligibility.
It adds fields without changing existing league/player identities. Run
`just migrate` before importing after this update. Existing live snapshot hashes
used a different representation, so the first normalized import may add a new
current observation; subsequent unchanged imports deduplicate normally.

## Next checkpoint

Implement ESPN against the same contract. Sleeper fixture tests now exercise
normalization and the interface; shared integration tests use normalized fixtures
for both provider identities without importing either adapter. The importer
validates references, coverage, periods, scores, and identity aliases before
persistence. Future adapters must translate their own response semantics.
