# League Lens

An open-source, self-hosted fantasy football dashboard for ESPN and Sleeper, with optional AI powered by your own credentials. The core dashboard should work without an AI subscription or mandatory paid data service. Yahoo is deferred.

## Install with Docker

Install Docker Desktop on Windows or macOS, or OrbStack on macOS. Clone or
download this repository, then open a terminal in the checkout:

```sh
docker compose up -d
```

The first run builds the app image locally and may take a few minutes. Open
`http://league-lens.localhost` in Chrome, Edge, or Firefox. Safari support is
unverified; use `http://localhost` in Safari instead. If port 80 is already in
use, put `LEAGUE_LENS_PORT=8080` in a `.env` file next to `compose.yaml`. Then
open `http://league-lens.localhost:8080` in Chrome, Edge, or Firefox, or
`http://localhost:8080` in Safari.

Open the app and click **Connect leagues** to connect ESPN or Sleeper. The
command-line importer remains available as an alternative:

```sh
docker compose exec app import sleeper --username YOUR_USERNAME --league-id YOUR_LEAGUE_ID
```

For ESPN, put `ESPN_S2` and `ESPN_SWID` in the same `.env` file, then run:

```sh
docker compose exec app import espn --league-id YOUR_LEAGUE_ID --season YEAR --team-id TEAM_ID
```

Update the app with `git pull && docker compose up -d --build`. Stop it with
`docker compose stop`; the named volumes retain your data. `docker compose down
-v` deletes all saved data.

## How we'll work

We will build one checkpoint at a time. After each checkpoint, stop, show what changed, report the checks performed and any limitations, and wait for your approval before starting the next checkpoint. Approval applies only to the next checkpoint, not the entire roadmap.

All changes must be made on feature branches and reviewed through pull requests. Never push directly to `main`. Keep only one active development branch and PR at a time; merge it before starting the next.

This README is the progress tracker. Check an item only when its acceptance criteria have been verified. Update the plan together if the scope changes. Do not publish, deploy to a public host, or choose a project license implicitly.

## Selected stack

| Area | Technology |
| --- | --- |
| Frontend | React, TypeScript, Vite |
| Navigation | TanStack Router |
| API client and cache | Connect TypeScript client and TanStack Query |
| UI | Tailwind CSS and shadcn/ui |
| Backend | Go, standard `net/http`, Connect |
| API contracts | Protocol Buffers and Buf |
| Database | PostgreSQL, pgx, sqlc |
| Database migrations | Goose |
| Background jobs (planned) | River open-source core, backed by Postgres |
| Packaging (local) | Docker Compose; Go serves the built frontend |
| AI (deferred) | OpenAI initially, behind a provider interface |

Node is used for frontend development and builds. The production application runs in Go. Start with one application process and Postgres; split out a worker only when needed. Authentication will live in the Go backend; its implementation will be reviewed before that checkpoint.

## Checkpoints

Completed items describe verified functionality on `main`. Unchecked items are
planned work, not a claim that implementation has started. Checkpoint numbers
remain stable even though the API and frontend work happened before owner setup.

- [x] **0. Verify access and initialize Git.** Read-only ESPN and Sleeper access verified; repository published with an MIT license and local credentials excluded.
- [x] **0a. Prepare the repository.** Conventional Commits, local hooks, editor/line-ending defaults, Git/Docker exclusions, and contribution guidance.
- [x] **1. Review the foundation design.** One local owner, full selected leagues, encountered players only, exact provider ID mappings, and available current-season history. See [database foundation](docs/designs/database-foundation.md) and [provider contracts](docs/designs/provider-contracts.md).
- [x] **2. Create the project skeleton and development workflow.** Go/Postgres and Vite/React entry points, configuration examples, dependency lockfiles, `just` commands, and Air reload. `just dev` runs frontend and API together; builds and frontend checks pass.
- [x] **3. Connect the frontend to Go.** Implemented through real dashboard RPCs instead of a demo health RPC. Committed Go and TypeScript bindings, separate generation commands, typed Connect calls, TanStack Query caching, and per-section loading/error/retry states.
- [x] **4. Add Postgres and migrations.** Goose, pgx, generated sqlc queries, ownership and history schema, atomic imports, health/readiness endpoints, and disposable database integration checks.

- [ ] **5. Add owner setup and credential protection.**
  - [x] Local owner selection through `LOCAL_OWNER_EMAIL`; this is an identity label, not authentication.
  - [ ] Review and implement protected owner setup, authentication, and server-side sessions with closed registration.
  - [x] Add encrypted provider credential storage. ESPN cookies use AES-256-GCM with a generated key file; a lost key only requires reconnecting ESPN ([design](docs/designs/league-connections.md#credential-storage)).
  - [ ] Verify unauthorized access is rejected and setup cannot be claimed without its token before supporting public exposure.

- [ ] **6. Complete ESPN connection setup.**
  - [x] Shared-provider Go adapter and CLI import: full league, rosters, records, encountered players, and available current-season matchup/roster history.
  - [x] Fixture coverage for normalization, invalid authentication, historical weeks, missing data, and multiweek scores; failures preserve previously imported data.
  - [x] Connect backend: ESPN fan-API league discovery, encrypted account cookies, owner-scoped import/removal, and sync using stored credentials.
  - [x] Add in-app ESPN league/team selection, bookmarklet or pasted cookies, and reconnect/error states ([design](docs/designs/league-connections.md#connecting-leagues)).

- [ ] **7. Complete Sleeper connection setup.**
  - [x] Shared-provider Go adapter and CLI import using a username and league ID, including team selection and available current-season history.
  - [x] Fixture coverage for mappings and missing data; public username lookup is selection, not proof of ownership.
  - [x] Connect backend username lookup, league discovery, and import through the shared importer.
  - [x] Add in-app username lookup and league/team selection ([design](docs/designs/league-connections.md#connecting-leagues)).

- [ ] **8. Build the combined dashboard.**
  - [x] Responsive shell, collapsible navigation, and persistent Light/Dark/System appearance.
  - [x] Owner-scoped `ListLeagues`, `GetLeague`, `GetTeamRoster`, and `ListMatchups` endpoints with modular handlers.
  - [x] Four overview section endpoints and connected UI: attention, matchups, player updates, and looking ahead.
  - [x] All-league overview with real imported scores, fetch timestamps, three attention items before expansion, and explicit unavailable news/schedule coverage. No mock data.
  - [x] Exact cross-provider player mapping with provider-ID fallback; only encountered players are stored, and uncertain identities are not joined by name.
  - [ ] Build Roster, Matchups, and Standings pages one at a time, with league selection where useful.
  - [ ] Show connection health and stale-data state. (Refresh now runs provider sync; see checkpoint 9.)
  - [x] Replace the empty overview with a Connect leagues entry point, manage connections in Settings, and show ESPN reconnect status.

- [ ] **9. Make synchronization automatic.** In progress: on-demand sync is implemented; scheduling remains.
  - [x] Reuse the import service behind a server-side sync operation (`SyncService`); keep the CLI as a development tool.
  - [x] On dashboard load, show saved data immediately and refresh stale provider data in the background.
  - [x] Make explicit Refresh fetch provider data, save it, and update the view; distinguish refreshing from a failed sync.
  - [ ] Define freshness intervals, duplicate prevention, bounded retries, and per-provider request limits.
  - [ ] Add persistent scheduled jobs using River so sync continues without an open browser and recovers after restart.
  - [ ] Verify a failed provider preserves its last successful data and does not stop other leagues from updating.

- [ ] **10. Add player news and supported alerts.**
  - [x] Empty starter-slot checks based on observed full/lineup snapshots; coverage explicitly remains partial.
  - [x] Choose permitted news sources with useful free coverage. NFL byes and kickoffs come from ESPN's public schedule, refreshed daily during sync into `nfl_games`.
  - [x] Match and deduplicate player news, with source links and publication times.
    - Player news uses ESPN's undocumented public fantasy feed for reporter-credited RotoWire blurbs; Sleeper has no news endpoint, X has no free read access, and Story roundups are skipped.
  - [x] Starter injury (Out/Doubtful/IR = action, Questionable = monitor) and bye checks from statuses captured at sync; alerts drop once the player's game kicks off. Unknown status or a missing schedule never produces an alert.
  - [ ] Upcoming-week lineup checks and bench projection warnings.
  - [ ] Verify league-rule handling and disclose missing coverage; paid feeds remain optional.

- [ ] **11. Add optional AI.** Deferred. Review the provider interface and context first, then add bring-your-own credentials, usage controls, sourced briefings, and roster-aware questions. The core dashboard must work without AI.

- [ ] **12. Package and verify self-hosting.**
  - [x] Local Postgres Compose service and documented development commands.
  - [x] Local Docker image and routing: `docker compose up` builds the image locally and serves the embedded frontend at `http://league-lens.localhost`, with Connect under `/api`, opt-in startup migrations, and a localhost-only Host check ([design](docs/designs/league-connections.md#installation)).
  - [ ] Backup/restore documentation and restore verification.
  - [ ] Verify a clean installation and restore; document HTTPS and complete owner protection before public exposure.

- [ ] **13. Prepare an open-source release.**
  - [x] MIT license, public repository, contributor guidance, and Conventional PR title checks.
  - [x] CI for Go checks, database integration, sqlc drift, and frontend lint/types/build.
  - [ ] Verify the production installation guide from a fresh checkout, review tracked files for secrets, and document provider limits and optional costs.
  - [ ] Add release artifacts/automation when self-hosting packaging is ready; deployment and release publication require explicit approval.

Win-probability charts, future matchup schedules, Yahoo, and a polished standalone
CLI remain deferred. The overview is an all-league starting point; it does not
require a global league selection.

## Repository layout

```text
web/                  React application and generated TypeScript bindings
cmd/server/           Go application entry point
internal/             Backend services, provider adapters, and persistence
proto/                Versioned API definitions
gen/                  Generated Go API bindings
db/                   SQL queries and migrations
internal/store/       Generated sqlc query bindings
internal/database/    Pool ownership and transaction boundary
docs/                 Approved designs and implementation notes
compose.yaml          Local Postgres and app services
Dockerfile            Multi-stage local app image
```

## Current state

The foundation, provider adapters, dashboard APIs, frontend shell, and connected
overview are merged on `main`. Both ESPN and Sleeper imports have been exercised
against real leagues, and the local overview was verified across three imported
leagues. No application deployment or production release has been performed.

Both adapters implement `fantasy.Provider`. The shared importer normalizes and
persists full leagues, encountered players, and available current-season history
atomically. DynastyProcess mappings are cached; unmapped players retain provider
identities. Earlier stored seasons are preserved.

The overview reads saved database observations. Opening it syncs leagues whose
data is older than 15 minutes in the background, and **Refresh** syncs every
attached league through the shared importer. In-app ESPN and Sleeper connection
is available from Home and Settings. ESPN credentials are encrypted per
connection for sync; `ESPN_S2` and `ESPN_SWID` are only the CLI import fallback.
Scheduled sync without an open browser (River) is also pending. News and
upcoming bye checks show unavailable coverage; Roster, Matchups, and Standings
pages remain placeholders. Authentication and AI are not implemented. The
local Docker image is built on the machine and is not published; HTTPS and public
deployment are not supported.

Local credentials and settings belong in ignored environment files, which are
excluded from Docker build contexts. Never commit them or include real
credentials or private league data in examples and fixtures. The API uses a
configured local owner rather than login sessions and should remain bound to
localhost.

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions and checks. Work on
one active branch/PR at a time; the roadmap does not authorize starting every
unchecked item.

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

No build or test command invokes a generator. `sqlc.yaml` generates committed bindings under `internal/store`; edit SQL inputs and run `just gen-db` rather than editing generated Go. See [the database design and import plan](docs/designs/database-foundation.md) for history semantics and the transaction boundary. Local owner-scoped data endpoints are implemented; public-facing authentication remains deferred.

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

`just gen-db` rejects other sqlc versions. After changing `db/queries` or migrations, run it explicitly and review the generated diff. Use `just gen-api` for Go protobuf/Connect bindings and `just gen-web` for TypeScript contracts. Ordinary startup, build, and tests never generate code.

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

The desktop-first shell lives in `web/`. Navigation and appearance work without
the backend; the connected overview requires the API and Postgres. It uses Vite, React, TypeScript, TanStack Router, Tailwind v4, and the
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

Overview is connected to the API; Roster, Matchups, and Standings remain separate
placeholder routes. Settings
contains a working appearance preference. Light, Dark, and System are supported;
the selected preference persists in this browser and follows operating-system
changes in System mode. The sidebar collapses and has a drawer at narrow widths.

`src/components/layout` owns shared navigation and the page frame;
`src/components/theme` owns appearance; `src/pages` owns page content;
`src/components/ui` contains shadcn primitives. Adding a real page replaces its
placeholder without rebuilding the shell. Overview section components live in `src/components/overview`, with typed API
access in `src/lib/api`. The dashboard endpoints and connected overview are
merged; remaining pages can use the existing read API foundation.

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

The backend exposes owner-scoped Connect reads consumed by the overview.
The service uses `LOCAL_OWNER_EMAIL` (default `owner@localhost`); this is a local
single-owner mode, not authentication. Keep the server bound to localhost.

The four overview RPCs are under `/leaguelens.v1.DashboardService/`:

| RPC | Current behavior |
| --- | --- |
| `GetOverviewAttention` | Starter checks from the latest observed snapshot: empty slots, injury status, and byes (from `nfl_games`). Alerts carry an `AlertKind` and an `AlertSeverity` (`ACTION` or `MONITOR`), sorted by severity then kickoff, and dropped after kickoff. Dismissed alerts are filtered and resurface on a new week or status. Coverage stays partial: statuses are as of the last sync. |
| `DismissLineupAlert` / `RestoreLineupAlert` | Dismiss or restore an alert for the configured local owner using its server-computed fingerprint. |
| `GetOverviewMatchups` | Selected teams, opponents, nullable actual scores, matchup status, and fetch timestamps. Matches scoring weeks to matchup rounds explicitly. |
| `GetOverviewPlayerUpdates` | Returns the latest saved item from the past seven days per rostered player, deduplicated across teams. Injury designations and starter injury news rank first, followed by other starters and bench players; results include categories, source links, and rostered teams. |
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
sources live in `db/queries/overview.sql`; overview messages live in
`proto/leaguelens/v1/overview.proto`, with league, roster, matchup, and player
messages in their corresponding `leagues.proto`, `rosters.proto`,
`matchups.proto`, and `players.proto` files. The service contract remains in
`dashboard.proto`.
Generated files can combine methods; application logic stays in focused files.

Run `just gen-api` for Go protobuf/Connect bindings and `just gen-db` for SQL
bindings. Run `just gen-web` for the frontend TypeScript contracts.

### Connected overview

The overview reads the four owner-scoped section endpoints using generated
Connect TypeScript contracts and TanStack Query. Vite proxies `/api` to the local
Go server at `127.0.0.1:8080`; use `just dev` to run both. `just dev-web` alone
shows per-section errors if the API is unavailable. The local Docker image
embeds and serves the app on localhost; public deployment and release artifacts
remain deferred. Any future reverse proxy must route `/api` to Connect.

Matchup scores are imported observations, not a live feed. Each card displays
its fetch timestamp. **Refresh** starts a provider sync; a status line shows
which leagues are syncing, and a failed league keeps showing its last saved data. Attention shows three alerts before expanding inline, labeled Action needed or Monitor; alerts can be dismissed with a brief Undo. Player updates show the top three saved RotoWire blurbs for rostered players; View all opens a side panel with the full list, filterable by league, player name, and injuries, with explicit limits for players ESPN cannot identify; looking-ahead checks retain the API's unavailable message. No sample data is used.

After editing proto contracts, run `just setup-web` then `just gen-web` to update
`web/src/gen`. Go generation remains independently available through `just gen-api`.
Generated TypeScript is type-checked but excluded from handwritten-code linting.
