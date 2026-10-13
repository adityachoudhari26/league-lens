# Provider import contracts

The provider boundary is `internal/fantasy.Provider`. It describes a league and
fetches a provider-independent payload. Adapter constructors receive credentials,
HTTP configuration, and provider-specific season selection. The importer receives
normalized data and owns transactions, sync status, deduplication, and player
identity resolution. Connection/team selection is application state, not provider
ownership metadata or authentication.

The contracts are ordinary Go types, written by hand. They are not Connect/Protobuf
API definitions, generated database bindings, or a plugin framework. Neither
provider has been adapted to them yet. The working Sleeper implementation remains
unchanged during this checkpoint.

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
schema. Wiring them may require persistence decisions for fields such as matchup
week spans and explicit byes. No database migration is introduced here.

## Next checkpoint

Adapt Sleeper first and run its existing fixture and integration tests through
this interface. Then implement ESPN against the same contract. Add runtime
validation with that wiring so both implementations enforce the documented
invariants before persistence. Avoid provider-specific branches in the shared
importer; each adapter translates its own response semantics.
