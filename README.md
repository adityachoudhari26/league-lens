# League Lens

An open-source, self-hosted fantasy football dashboard for ESPN and Sleeper, with optional AI powered by your own credentials. The core dashboard should work without an AI subscription or mandatory paid data service. Yahoo is deferred.

## How we'll work

We will build one checkpoint at a time. After each checkpoint, stop, show what changed, report the checks performed and any limitations, and wait for your approval before starting the next checkpoint. Approval applies only to the next checkpoint, not the entire roadmap.

All changes must be made on feature branches and reviewed through pull requests. Never push directly to `main`.

This README is the progress tracker. Check an item only when its acceptance criteria have been verified. Update the plan together if the scope changes. Do not publish, deploy to a public host, or choose a project license implicitly.

## Selected stack

| Area | Technology |
| --- | --- |
| Frontend | React, TypeScript, Vite |
| Navigation | TanStack Router |
| API client and cache | Connect Query and TanStack Query |
| UI | Tailwind CSS and shadcn/ui |
| Backend | Go, standard `net/http`, Connect |
| API contracts | Protocol Buffers and Buf |
| Database | PostgreSQL, pgx, sqlc |
| Database migrations | Goose |
| Background jobs | River open-source core, backed by Postgres |
| Packaging | Docker Compose; Go serves the built frontend |
| AI | OpenAI initially, behind a provider interface |

Node is used for frontend development and builds. The production application runs in Go. Start with one application process and Postgres; split out a worker only when needed. Authentication will live in the Go backend; its implementation will be reviewed before that checkpoint.

## Checkpoints

- [x] **0. Verify access and initialize Git.** Read-only ESPN and Sleeper requests successfully retrieved the selected teams, rosters, scoring settings, and current matchups. Git is initialized on `main`, and `.env.local` is ignored. These were manual connection checks; reusable connectors are still to be built.

- [x] **0a. Prepare the repository.** Add Conventional Commit guidance and a local commit-message hook, editor and line-ending defaults, Git and Docker exclusions, and contribution instructions. **Done when:** commit validation accepts valid messages and rejects invalid ones, credentials remain excluded, and housekeeping files pass checks. Stop for review before checkpoint 1.

- [x] **1. Review the scaffold design.** Agree on directory layout, local development commands, configuration, and the minimal frontend-to-backend health flow. The owner approved an infrastructure/database-first slice in chat: full selected leagues, encountered players only, one owner, and current-season history backfill. See [database foundation](docs/designs/database-foundation.md) for the recorded design and next steps. Frontend, Connect, and AI are deferred.

- [ ] **2. Create the project skeleton.** Add React/Vite and Go entry points, dependency manifests and lockfile, basic UI styling, configuration validation, and secret-free configuration examples. **Done when:** the frontend builds, the Go backend builds, and documented development commands work. No fantasy integration yet.

- [ ] **3. Connect the frontend to Go.** Define a small health service in Protobuf, configure reproducible code generation, and call it from the frontend through Connect Query. **Done when:** the page displays the real backend response and a useful error when the backend is unavailable; generated bindings reproduce cleanly.

- [x] **4. Add Postgres and migrations.** Add a local database service, Goose migrations, pgx connections, and sqlc queries. Design ownership for users, connections, leagues, and selected teams. Separate process health from database readiness. **Done when:** an empty database migrates successfully, a database round trip works, and an unavailable database is reported accurately.

- [ ] **5. Add owner setup and credential protection.** Review and implement owner authentication, protected setup, server-side sessions, and encrypted credential storage. Keep registration closed by default. **Done when:** unauthorized requests cannot access protected data, secrets do not appear in responses or logs, and setup cannot be claimed without its token. Document encryption-key backup requirements.

- [ ] **6. Build the ESPN connector.** Backend adapter and command implemented and verified; UI configuration/reconnect states remain deferred. Turn the verified read-only flow into reusable Go code; normalize league settings, teams, roster slots, players, and matchups. Add UI configuration and reconnect/error states. **Done when:** the selected ESPN team imports correctly, fixture tests verify normalization, and an expired or invalid connection leaves the last successful snapshot intact.

- [x] **7. Build the Sleeper connector.** Look up a username, select a league/team, and normalize the same data categories. Treat public username lookup as selection, not proof of account ownership. **Done when:** the selected Sleeper team imports correctly and fixture tests cover its mappings and missing-data cases.

- [ ] **8. Build the combined dashboard.** Display both teams, rosters, matchups, scoring context, connection state, manual refresh, and last successful sync times. Introduce explicit cross-provider player ID mappings; do not merge by name alone. **Done when:** both real teams appear correctly, unresolved player mappings stay explicit, and stale or missing data is visibly distinguished from fresh data.

- [ ] **9. Add reliable scheduled sync.** Use River for persistent jobs, bounded retries, duplicate prevention, and per-provider request limits. **Done when:** sync runs without an open browser, resumes after restart, and a failed provider does not stop the other connector. Expose job failures in the UI without exposing credentials.

- [ ] **10. Add player news and basic alerts.** Review available source permissions and coverage before selecting a default source. Match stories to players, deduplicate updates, link to sources, and add supported bye/lineup alerts. **Done when:** relevant updates include source and publication time, alerts honor league rules, and missing coverage is disclosed. Paid feeds remain optional.

- [ ] **11. Add optional AI.** Add user-provided OpenAI credentials, model settings, usage controls, a sourced briefing, and roster-aware questions. Review the context and provider interface before implementation. **Done when:** answers use actual selected-team data, unsupported claims are rejected or qualified, credentials remain private, and the core dashboard works with AI disabled or unavailable.

- [ ] **12. Package and verify self-hosting.** Complete the production Docker image and Compose setup, persistent volumes, migration workflow, health checks, and installation/upgrade/backup documentation. **Done when:** a clean installation starts using documented steps, the built frontend is served by Go, and backup/restore is exercised. Document HTTPS configuration for public exposure.

- [ ] **13. Prepare the open-source release.** The MIT license is in place. Add contributor guidance and CI, inspect tracked files for secrets, and document provider limits and optional costs. **Done when:** a fresh checkout passes the required checks and follows the installation guide. Publishing the repository is a separate explicit step.

## Proposed repository layout

```text
web/                  React application and generated TypeScript bindings
cmd/server/           Go application entry point
internal/             Backend services, connectors, auth, and jobs
proto/                Versioned API definitions
gen/                  Generated Go API bindings
db/                   SQL queries and migrations
internal/store/       Generated sqlc query bindings
internal/database/    Pool ownership and transaction boundary
docs/                 Approved designs and implementation notes
compose.yaml          Self-hosted services
Dockerfile            Frontend build and Go runtime packaging
```

## Current state

The repository includes the locally implemented Go/Postgres foundation described below, plus the earlier manual provider connection checks. Existing private provider credentials remain untouched. No application deployment has been performed.

Credentials live in the ignored `.env.local` file. Never commit that file, paste its contents into chat, or include it in a Docker build context. Add appropriate Docker exclusions when packaging begins. Examples and test fixtures must contain no real credentials or private league data.

**Current:** Sleeper and ESPN implement the shared `fantasy.Provider` contract. The importer consumes normalized league data; provider
HTTP formats stay inside adapters. See [provider contracts](docs/designs/provider-contracts.md).

Included: a reusable provider-independent import service, a temporary flag-based import command,
current rosters and team records, available current-season weekly rosters/matchups,
cached DynastyProcess identity lookup, and atomic persistence with sync status.
Authentication, frontend/Connect work, scheduling, and AI remain deferred.

Sleeper now exercises full-league import and current-season backfill. Existing prior-season data is preserved. Generated queries and transaction behavior are tested against disposable Postgres.

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions and local hook setup.

## Development tools

Use `just` to list the command menu. Install it with `brew install just` on macOS or follow [the official installation guide](https://just.systems/man/en/packages.html). Then run the setup sequence below. `just tools` installs Air v1.65.3 under the ignored project `bin/` directory, without changing your global Air installation.

`just dev-api` uses Air to rebuild and restart Go after source or embedded SQL changes. It stops the old server on a compile error and sends an interrupt for graceful shutdown. SQL edits trigger a rebuild only; migrations are always explicit. Development commands do not run generation; use `just gen-db` explicitly when changing database queries. Use `just serve` for a plain run. Use `just dev` to run the frontend and API together, or `just dev-web` for the frontend alone; see the frontend shell section below.

Development settings live in `.env.development` (created with owner-only permissions). Restart the development command after changing that file. The existing `.env.local` provider credentials and local VS Code settings remain separate and ignored. Use `just setup` safely on an existing checkout: it never overwrites configuration.

## Local database foundation

Prerequisites: Go 1.26+, just, and Docker Compose. Node is not required for this slice.

```sh
just setup
just tools
just db-up
just migrate
just dev-api
```

The database port is loopback-only. The password shown is a local development default; configure your own values before any remote deployment. `just` loads `.env.development`; exported shell values take precedence. Direct Go commands read exported environment variables only. Neither just nor Air loads `.env.local`. Do not overwrite the existing provider credential file with `.env.example`.

- `GET http://127.0.0.1:8080/healthz`: process liveness.
- `GET http://127.0.0.1:8080/readyz`: database connectivity, not migration/auth/provider readiness.
- `just test`, `just check`, `just build`: unit tests, static checks, and Go binaries.
- `just test-db`: starts a disposable Postgres container, tests migrations/schema/queries, and removes the container afterward. It does not use your development database.
- `just db-stop`: stop Postgres while retaining its volume.

No build or test command invokes a generator. `sqlc.yaml` generates committed bindings under `internal/store`; edit SQL inputs and run `just gen-db` rather than editing generated Go. See [the database design and import plan](docs/designs/database-foundation.md) for history semantics and the transaction boundary. Public-facing authentication and data endpoints are not part of this scaffold.

## License

This project is licensed under the [MIT License](LICENSE). You may use, modify, redistribute, and commercially host the software subject to its license terms. External services and data remain subject to their own terms.

## Typed database queries

Generation uses sqlc **v1.30.0**, the version recorded in the generated files. Install that version if it is not already on your PATH:

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
# Ensure the Go bin directory (normally $HOME/go/bin) is on PATH.
sqlc version
just gen-db
```

`just gen-db` rejects other sqlc versions. After changing `db/queries` or migrations, run it explicitly and review the generated diff. Buf and other generators remain deferred. Ordinary startup, build, and tests never generate code.

`internal/database` owns the pool and exposes `Queries()` for individual operations and `InTx(ctx, callback)` for atomic writes. The callback receives transaction-bound generated queries. Use those queries for every operation in the transaction and do not retain them afterward. Errors, panics, and cancellation roll back; callbacks are not automatically retried. Do not call `Queries()` or nest `InTx` from inside the callback. Network fetches belong outside the transaction.

`just test-db` now runs both schema checks and typed-query transaction tests in a disposable database. No real provider credentials are needed.

## Import a Sleeper league locally

```sh
just setup        # once; creates ignored .env.development
just db-up
just migrate
just import-sleeper YOUR_USERNAME YOUR_LEAGUE_ID
```

For a different local owner identity, pass an email as the third argument. The
stable default is `owner@localhost`; this is a local identity label, not a login.
Public Sleeper usernames select teams; they do not authenticate ownership.

With `DATABASE_URL` already exported, the equivalent command is:

```sh
go run ./cmd/import sleeper --username YOUR_USERNAME --league-id YOUR_LEAGUE_ID
```

The command does not load dotenv files itself; `just` does. It does not migrate
automatically. Use `--help` for options including `--mapping-cache`. The default
mapping cache is the OS user cache under `league-lens/playerids.csv`, refreshed
after 24 hours. An unavailable mapping dataset falls back to Sleeper identities;
unknown player names display as `Sleeper <id>`. Only encountered players enter
Postgres, and defenses remain distinct from people. Mapping conflicts fail the
import rather than silently joining different existing players.

Imports fetch the current NFL season through the current week (through week 18
in postseason), including all teams and available weekly rosters. Current
standings inputs are retained as roster records; past standings are not inferred.
Historical missing benches produce lineup-only snapshots. Missing weeks are
skipped and previous evidence retained. Current matchups have `unknown` status
because the weekly response does not establish whether games have started.
Commissioner score overrides, including zero, take precedence.

Reruns update corrected matchups/history and add current observations only when
roster content or records change. Provider requests run outside transactions;
short database write phases serialize using an advisory lock to protect shared
player identities. A failed write rolls back the whole season import and records
a failed sync; its last successful data remains available. A newer completed
import prevents an older in-flight import from overwriting it. Initial user or
league lookup failures happen before a sync target exists and return directly.
An interrupted process can leave a running sync record; recovery of abandoned
runs belongs to the future job runner.

The temporary command calls `internal/importer`, which can later be reused by
Connect handlers or scheduled jobs. This checkpoint does not fetch news,
transactions, draft picks, future schedules, or previous seasons.

Sources: [Sleeper API](https://docs.sleeper.com/) and
[DynastyProcess player ID mappings](https://github.com/dynastyprocess/data).

### Provider contract migration

Run `just migrate` after this update. The additive migration retains normalized
team records/ranks, matchup scoring-week spans and byes, and unknown eligibility.
Existing historical data is retained. The first import after the change may
create a fresh current observation because snapshot hashing now uses the shared
model; later unchanged imports deduplicate as before. ESPN uses the same schema; no additional migration is needed for its adapter.


## Import a private ESPN league locally

After starting Postgres and applying migrations, export `ESPN_S2` and `ESPN_SWID`
from your private local credentials, then run:

```sh
just import-espn YOUR_LEAGUE_ID 2026 YOUR_TEAM_ID
```

With `DATABASE_URL` already exported, the equivalent command is:

```sh
go run ./cmd/import espn --league-id YOUR_LEAGUE_ID --season 2026 --team-id YOUR_TEAM_ID
```

The season is explicit because ESPN reuses league IDs. Team selection is a local
preference, not application authentication. Credentials are environment-only;
the command does not read `.env.local` automatically. `just` loads only
`.env.development`. Do not put cookie values in command arguments or commit them.
The connection stores a hash-derived account identifier, not the SWID cookie.

The adapter imports all teams, current records/rosters, and available earlier
weekly rosters in the selected season. ESPN must return the requested historical
week; a mismatched week fails the import. Missing roster data remains unknown,
while an explicit empty entry list means an empty roster. Historical snapshots
do not borrow current standings. Matchup totals retain their scoring-week spans,
including multiweek rounds; future rounds are deferred. Undecided matchups retain
unknown status because the response does not prove games have started. Missing opponents are
not automatically classified as byes. Partial schedules cannot delete unseen
pairings. Unknown roster slots retain their ESPN slot ID.

Player identities use exact ESPN IDs plus the shared mapping dataset. Defense
aliases use ESPN franchise IDs. Unsupported scoring rules and position-specific
overrides remain in supplemental settings. Credentials and member data are never
included in those settings. There are no automatic retries; HTTP/auth failures
return a sanitized error and leave previously imported league data intact.

ESPN's fantasy API is undocumented and may change. The adapter follows the
[community ESPN client](https://github.com/cwendt94/espn-api) and verified read-only
responses. Fixture tests cover authentication failures, redirects, history,
multiweek scores, defense identity, and missing data.


## Frontend shell

The desktop-first shell lives in `web/` and runs independently of the API and
Postgres. It uses Vite, React, TypeScript, TanStack Router, Tailwind v4, and the
standard shadcn sidebar/menu components (Base UI primitives). Geist is bundled
locally; the UI does not depend on a font CDN.

With Node 22.12+ (Node 24.19.0 is recorded in `web/.node-version`) and pnpm 11.15.1:

```sh
just setup-web
just dev
```

Open **http://127.0.0.1:5173**. `just dev` runs Vite and the Go API together;
one Ctrl+C stops both, and either process exiting stops its sibling. It requires
`just setup` and `just tools` once for API configuration and Air. Use `just db-up`
when you need database-backed API behavior. `just dev-web` runs only the frontend
and requires no database/API setup. `just check-web` runs lint and type checks, and `just build-web`
produces `web/dist`. The development port is fixed; an occupied port produces an
error instead of silently opening a different address. These commands prefer an
optional ignored `bin/node` before your normal PATH; no machine-specific runtime
path is committed.

Overview, Roster, Matchups, and Standings are separate placeholder routes. Settings
contains a working appearance preference. Light, Dark, and System are supported;
the selected preference persists in this browser and follows operating-system
changes in System mode. The sidebar collapses and has a drawer at narrow widths.

`src/components/layout` owns shared navigation and the page frame;
`src/components/theme` owns appearance; `src/pages` owns page content;
`src/components/ui` contains shadcn primitives. Adding a real page replaces its
placeholder without rebuilding the shell. The pages currently display no league
data and make no API calls. The dashboard API work is preserved separately on
`feat/dashboard-api`; connecting it and building each page are later checkpoints.

The shell follows the official [shadcn Vite setup](https://ui.shadcn.com/docs/installation/vite)
and [theme-provider approach](https://ui.shadcn.com/docs/dark-mode/vite), including
listening for system appearance changes.

### Frontend linting

Run `just lint-web` for lint only, or `just fix-web` to apply automatic fixes.
`just check-web` runs ESLint with zero warnings allowed, then TypeScript.
Frontend CI installs from the frozen lockfile and runs checks and a production build.

ESLint checks recommended JavaScript and type-aware TypeScript rules (including
unsafe values, unused variables, and unhandled promises), React hooks, JSX
accessibility, and consistent type imports. These checks cover shadcn components
as well as app code. Layout and visual accessibility still need browser review.
We avoid function-length limits: small, meaningful helpers are encouraged.

TypeScript is pinned to 6.0.3 for typescript-eslint compatibility. ESLint remains
on 9 until jsx-a11y supports 10; upgrade these together when their supported
versions align. No formatting rules or formatter have been added in this step.

## Overview read API

This backend branch adds owner-scoped Connect reads independently of the frontend.
The service uses `LOCAL_OWNER_EMAIL` (default `owner@localhost`); this is a local
single-owner mode, not authentication. Keep the server bound to localhost.

The four overview RPCs are under `/leaguelens.v1.DashboardService/`:

| RPC | Current behavior |
| --- | --- |
| `GetOverviewAttention` | Empty starter-slot checks from full/lineup observed snapshots; marks coverage partial because injury and bye checks are unavailable. |
| `GetOverviewMatchups` | Selected teams, opponents, nullable actual scores, matchup status, and fetch timestamps. Matches scoring weeks to matchup rounds explicitly. |
| `GetOverviewPlayerUpdates` | Returns unavailable coverage and no invented articles until a news source is connected. |
| `GetOverviewLookingAhead` | Returns unavailable coverage until NFL bye schedules are connected. |

Each RPC accepts an empty request. Attention and matchup queries use selected
teams in the owner's latest imported season, ignore disabled connections, and
have a five-second read transaction deadline. Reads are capped at 100 selected
teams; exceeding the cap fails rather than silently dropping teams. No provider
requests, scheduled syncing, probabilities, or live-score claims are included.
An absent score is unknown; zero is a real score. No snapshot is unavailable,
not a bye or an empty roster. The UI should display observation/fetch timestamps.

Example:

```sh
curl --fail-with-body http://127.0.0.1:8080/leaguelens.v1.DashboardService/GetOverviewMatchups \
  -H 'Content-Type: application/json' -H 'Connect-Protocol-Version: 1' -d '{}'
```

The existing local API foundation is included: `ListLeagues`, `GetLeague`,
`GetTeamRoster`, and `ListMatchups`. Their handlers are separate from the new
`overview_attention.go`, `overview_matchups.go`, `overview_player_updates.go`,
and `overview_looking_ahead.go` files in `internal/dashboard`.
`overview.go` contains only shared overview reading/message helpers. Query
sources live in `db/queries/overview.sql`; section messages live in
`proto/leaguelens/v1/overview.proto`. The service contract is in `dashboard.proto`.
Generated files can combine methods; application logic stays in focused files.

Run `just gen-api` for Go protobuf/Connect bindings and `just gen-db` for SQL
bindings. No frontend or TypeScript generation is required by this branch.

### Connected overview

The overview reads the four owner-scoped section endpoints using generated
Connect TypeScript contracts and TanStack Query. Vite proxies `/api` to the local
Go server at `127.0.0.1:8080`; use `just dev` to run both. `just dev-web` alone
shows per-section errors if the API is unavailable. A deployed server/reverse
proxy will also need to route `/api` to Connect; production packaging is deferred.

Matchup scores are imported observations, not a live feed. Each card displays
its fetch timestamp; **Refresh view** reloads the database view without syncing
providers. Attention shows three alerts before expanding inline. News and bye
checks retain the API's explicit unavailable messages. No sample data is used.

After editing proto contracts, run `just setup-web` then `just gen-web` to update
`web/src/gen`. Go generation remains independently available through `just gen-api`.
Generated TypeScript is type-checked but excluded from handwritten-code linting.
