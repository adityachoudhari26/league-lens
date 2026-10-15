# Roadmap and checkpoints

This file is the project's progress tracker. The [README](../README.md#roadmap) has a short summary.

## How we'll work

We will build one checkpoint at a time. After each checkpoint, stop, show what changed, report the checks performed and any limitations, and wait for your approval before starting the next checkpoint. Approval applies only to the next checkpoint, not the entire roadmap.

All changes must be made on feature branches and reviewed through pull requests. Never push directly to `main`. Keep only one active development branch and PR at a time; merge it before starting the next.

This file is the progress tracker. Check an item only when its acceptance criteria have been verified. Update the plan together if the scope changes. Do not publish, deploy to a public host, or choose a project license implicitly.

## Checkpoints

Completed items describe verified functionality on `main`. Unchecked items are
planned work, not a claim that implementation has started. Checkpoint numbers
remain stable even though the API and frontend work happened before owner setup.

- [x] **0. Verify access and initialize Git.** Read-only ESPN and Sleeper access verified; repository published with an MIT license and local credentials excluded.
- [x] **0a. Prepare the repository.** Conventional Commits, local hooks, editor/line-ending defaults, Git/Docker exclusions, and contribution guidance.
- [x] **1. Review the foundation design.** One local owner, full selected leagues, encountered players only, exact provider ID mappings, and available current-season history. See [database foundation](designs/database-foundation.md) and [provider contracts](designs/provider-contracts.md).
- [x] **2. Create the project skeleton and development workflow.** Go/Postgres and Vite/React entry points, configuration examples, dependency lockfiles, `just` commands, and Air reload. `just dev` runs frontend and API together; builds and frontend checks pass.
- [x] **3. Connect the frontend to Go.** Implemented through real dashboard RPCs instead of a demo health RPC. Committed Go and TypeScript bindings, separate generation commands, typed Connect calls, TanStack Query caching, and per-section loading/error/retry states.
- [x] **4. Add Postgres and migrations.** Goose, pgx, generated sqlc queries, ownership and history schema, atomic imports, health/readiness endpoints, and disposable database integration checks.

- [ ] **5. Add owner setup and credential protection.**
  - [x] Local owner selection through `LOCAL_OWNER_EMAIL`; this is an identity label, not authentication.
  - [ ] Review and implement protected owner setup, authentication, and server-side sessions with closed registration.
  - [x] Add encrypted provider credential storage. ESPN cookies use AES-256-GCM with a generated key file; a lost key only requires reconnecting ESPN ([design](designs/league-connections.md#credential-storage)).
  - [ ] Verify unauthorized access is rejected and setup cannot be claimed without its token before supporting public exposure.

- [ ] **6. Complete ESPN connection setup.**
  - [x] Shared-provider Go adapter and CLI import: full league, rosters, records, encountered players, and available current-season matchup/roster history.
  - [x] Fixture coverage for normalization, invalid authentication, historical weeks, missing data, and multiweek scores; failures preserve previously imported data.
  - [x] Connect backend: ESPN fan-API league discovery, encrypted account cookies, owner-scoped import/removal, and sync using stored credentials.
  - [ ] Add in-app league/team selection, credential configuration, and reconnect/error states. Proposed: bookmarklet or pasted cookies and Reconnect when cookies are rejected ([design](designs/league-connections.md#connecting-leagues)).

- [ ] **7. Complete Sleeper connection setup.**
  - [x] Shared-provider Go adapter and CLI import using a username and league ID, including team selection and available current-season history.
  - [x] Fixture coverage for mappings and missing data; public username lookup is selection, not proof of ownership.
  - [x] Connect backend username lookup, league discovery, and import through the shared importer.
  - [ ] Add in-app username lookup and league/team selection ([design](designs/league-connections.md#connecting-leagues)).

- [ ] **8. Build the combined dashboard.**
  - [x] Responsive shell, collapsible navigation, and persistent Light/Dark/System appearance.
  - [x] Owner-scoped `ListLeagues`, `GetLeague`, `GetTeamRoster`, and `ListMatchups` endpoints with modular handlers.
  - [x] Four overview section endpoints and connected UI: attention, matchups, player updates, and looking ahead.
  - [x] All-league overview with real imported scores, fetch timestamps, three attention items before expansion, and explicit unavailable news/schedule coverage. No mock data.
  - [x] Exact cross-provider player mapping with provider-ID fallback; only encountered players are stored, and uncertain identities are not joined by name.
  - [ ] Build Roster, Matchups, and Standings pages one at a time, with league selection where useful.
  - [ ] Show connection health and stale-data state. (Refresh now runs provider sync; see checkpoint 9.)
  - [ ] Replace the empty overview with a Connect leagues entry point, and manage connections in Settings.

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
  - [x] Local Docker image and routing: `docker compose up` builds the image locally and serves the embedded frontend at `http://league-lens.localhost`, with Connect under `/api`, opt-in startup migrations, and a localhost-only Host check ([design](designs/league-connections.md#installation)).
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
attached league through the shared importer. ESPN credentials are encrypted per
connection for sync; `ESPN_S2` and `ESPN_SWID` remain a fallback for existing
environment-based imports. The connection backend supports ESPN and Sleeper
discovery, import, and league removal; the in-app connection UI is next.
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

See [CONTRIBUTING.md](../CONTRIBUTING.md) for commit conventions and checks. Work on
one active branch/PR at a time; the roadmap does not authorize starting every
unchecked item.
