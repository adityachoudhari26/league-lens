# Database foundation

Status: foundation merged; typed sqlc queries and transaction wiring implemented locally in a follow-up checkpoint. sqlc generation is authorized; pushes and PRs for the follow-up are not. Frontend, Connect handlers, authentication, connectors, and AI are outside this slice.

## Runtime

A Go process uses pgx to connect to Postgres. The local Compose file starts only Postgres, bound to loopback. The server binds to `127.0.0.1:8080` by default. It exposes process liveness at `/healthz` and database connectivity at `/readyz`. Readiness does not yet certify migrations or provider connections. There are no public league or credential endpoints.

Migrations run through a separate explicit command using Goose and embedded SQL. Startup never runs migrations automatically. `go:embed` is a compiler feature, not a code-generation command. River, Vite, and Connect will be added in later slices; they are not required to inspect or test the schema.

## Domain model

| Table | Responsibility |
| --- | --- |
| users | App identities; authentication credentials/sessions are not implemented |
| connections | Owner and provider identity; opaque encrypted-credential storage reserved for the auth/connector slice |
| league_seasons | Shared provider league for a specific season and current settings JSON |
| connection_leagues | Owner/provider-consistent access and last successful sync |
| teams | All fantasy teams in imported league seasons |
| user_teams | Personal team selection within an accessible league |
| players | Minimal records created only for encountered people or team defenses |
| player_external_ids | Provider IDs mapped to internal player UUIDs, with provenance |
| sync_runs | Initial, refresh, or per-period backfill attempts and sanitized error codes |
| team_snapshots | Observed or historical roster/standing observations with coverage and time metadata |
| roster_entries | Players and source-specific lineup slots for a snapshot |
| matchups | Current known schedule/result for each provider matchup and period |

Provider IDs are strings. League identities are unique by provider, external ID, and season. Composite foreign keys reject cross-owner connection links, cross-provider leagues, and cross-season team/snapshot/matchup links. Future API authorization must still check the signed-in user; foreign keys are not authentication. User-facing queries require a user ID and check connected-league access.

## Players

Cache the DynastyProcess mapping resource when the connector slice is implemented; do not insert its full catalog into Postgres. Lookup encountered ESPN/Sleeper IDs against it. Reuse an existing player when a linked provider ID already exists. Otherwise insert one minimal player record and its known IDs in a transaction.

A missing mapping creates a provider-specific identity. A conflict must not silently remap an existing external ID. `AddPlayerExternalID` deliberately does nothing on conflict and returns no row: the caller must re-read and either reuse the winning identity or roll back its tentative player insert. Reconciliation of already-created duplicates is a later, explicit operation.

Defenses use `entity_type=team_defense`; never merge by display name. Store positions and lineup slots in roster entries because eligibility belongs to the platform and league, not a universal player attribute.

## Current-season history

On first connection, fetch the current league and all current-season historical periods the provider actually exposes. This schema supports that workflow; network backfill is not implemented yet.

- `team_snapshots.period` is the provider scoring period for that roster/lineup.
- `matchups.period` is the provider matchup period, which can span multiple scoring periods.
- `origin` distinguishes live `observed` records from `provider_history` fetched later.
- `observed_at` is when the data was fetched. `effective_at` is nullable: do not invent a date if the provider only identifies a week.
- `coverage` is `full`, `lineup`, or `partial`. A historical starting lineup cannot imply an empty bench.
- `record_json` is nullable. Do not attach today's standings to earlier periods when historical standings are unavailable.
- Historical `source_key` is stable for the provider record and coverage, such as `week-3:lineup`; retries/corrections update that record rather than duplicate it. Different coverage gets a separate key.
- Live observations get a fresh observation key when roster/lineup/record content changes, or the scoring period changes. Compare the canonical content hash to the latest successful observed snapshot. A sequence A → B → A keeps all three observations.
- Snapshot entry replacement and metadata update must occur in the same transaction. Reject incomplete responses before replacing a full snapshot.
- Live scores update the matchup row; there is no score-tick history. Unknown scores stay NULL and scores use decimals. Adapters must supply stable matchup IDs (including deterministic bye IDs if needed).
- New seasons create separate league-season rows. Preserve prior season data; do not automatically delete it or synthesize missing past lineups.

## Import transaction boundary

1. Resolve the owner's connection and connected league; create a running sync attempt.
2. Fetch and validate provider data outside a database transaction.
3. Open a transaction and serialize imports for the league (for example, lock its `league_seasons` row).
4. Upsert teams, resolve encountered players, write changed/current or historical snapshots and roster entries, and upsert matchups.
5. Mark that sync successful and update its connection-league sync time in the same transaction; commit.
6. On failure, roll back data changes and mark the attempt failed separately using a sanitized code, never raw provider responses or cookies.

Backfill should use one attempt per historical period so one unavailable week does not discard successful weeks. Interrupted running attempts will need recovery when jobs are introduced. Data must never be marked successful on a partial fetch represented as a full response.

## Development commands

Go 1.26+, just, and Docker Compose are sufficient for this slice; the machine's broken Node installation is not used. Run `just setup` to copy `.env.example` to `.env.development` without overwriting an existing file. Do not overwrite or source the existing provider credential file into a client build.

```sh
just setup
just tools
just db-up
just migrate
just dev-api
```

The sample password is for a loopback-only development database. If changing Compose credentials or port, update DATABASE_URL accordingly. Keep TLS enabled for remote databases. `just db-stop` preserves the named volume. `just migrate-down` explicitly rolls back one migration and can delete application data; it is not an ordinary stop command.

```sh
just test
just check
just build
# Creates and removes its own disposable database container.
just test-db
```

Integration tests validate migrations up/repeat/down/up, source-query preparation, identity uniqueness, access filtering, cross-season protection, partial historical coverage, null scores, and idempotent history corrections. Synthetic fixture data never contains real league details.

## Typed queries and explicit generation

`sqlc.yaml` and `db/queries/*.sql` generate committed Go bindings under `internal/store` with sqlc v1.30.0. `just gen-db` verifies that version and runs generation explicitly; startup, builds, and tests do not run it. Regenerating unchanged inputs must leave the generated files unchanged. Buf and other generators remain deferred.

`internal/database.Open` creates a lazy pgx pool and binds generated queries. The server's readiness endpoint uses its Ping method. Application writes can use `InTx`: it supplies transaction-bound queries, commits on success, and defers rollback using an independent bounded context so cleanup survives request cancellation or panic. Panics propagate after rollback. Commit errors propagate; callbacks are not retried automatically. Callers must not retain transaction queries, use the nontransactional pool from inside the callback, or nest transactions.

Tests exercise typed writes, commit visibility, rollback on callback/SQL failures, cancellation and panic cleanup with a one-connection pool, and query generation reproducibility. Import-specific league locking and scheduling remain part of connector implementation.

## Next implementation slice

Review the typed query and transaction layer. Authentication and provider import services are separate future checkpoints. Initial-season backfill is part of connector implementation, not an already-running feature. No UI or AI work is needed to review this foundation.
