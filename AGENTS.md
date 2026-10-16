# Repository instructions

- Work on a feature branch before editing files. Never commit or push directly to `main`.
- Push feature branches and use pull requests. Do not merge without explicit user approval.
- Follow the checkpoint workflow in docs/roadmap.md: complete only the authorized checkpoint, report verification, and pause for review.
- Follow CONTRIBUTING.md, including Conventional Commit subjects.
- Never print, commit, or include local credentials in fixtures or build contexts. `.env.local` is private.

## Current scope

- sqlc generation is authorized for database queries via `just gen-db` (v1.30.0). Frontend scaffolding and shadcn component generation are authorized for the shell. Buf generation is authorized for dashboard contracts via `just gen-api`. Other Go/scaffold generators still require explicit authorization.
- Current implementation scope includes the Go/Postgres foundation and provider-independent import contracts with the Sleeper and ESPN adapters/commands. The Vite/React frontend shell and shadcn components are authorized. Owner-scoped Connect dashboard and overview endpoints are authorized. Frontend overview integration is authorized in its separate PR, including TypeScript contract generation via `just gen-web`. Checkpoint 9 provider synchronization is authorized: server-side sync through the shared importer, stale refresh on dashboard load, and an explicit Refresh that contacts providers. River-backed scheduling belongs to a later Checkpoint 9 PR. Checkpoint 10 starter checks are authorized: injury status and NFL team captured during sync (ESPN roster fields, Sleeper per-player `/players/nfl/{id}` lookups for selected teams), a daily ESPN public NFL schedule stored in `nfl_games`, and injury/bye alerts in `GetOverviewAttention`. Checkpoint 10 player news is authorized: a source-agnostic `news.Source` interface with an ESPN fantasy player news (RotoWire blurbs) implementation, fetched during sync into `player_news`, ranked rule-based in `GetOverviewPlayerUpdates`. Projection-based alerts remain deferred. AI assistant BYOK settings, the `llm.Provider` seam, and server-side chat are authorized.

Dismissible attention alerts are authorized: owner-scoped `alert_dismissals` keyed by an alert fingerprint (ID, week, status), filtered in `GetOverviewAttention`, with Dismiss/Restore RPCs.

The Matchups detail backend is authorized: a `matchup_lineups` table with per-player league-scored points from ESPN `mBoxscore` and Sleeper `/matchups`, Sleeper availability lookups extended to the current opponent, and an owner-scoped `GetMatchupDetail` RPC for the owner's matchup in one round. Win probability remains deferred.

Matchup projections are authorized: per-player league-scored projections stored as `matchup_lineups.projected_points` (latest only, replaced for each week a sync reads), from ESPN `mBoxscore` projected stat rows and, for Sleeper, the `/projections/nfl/{season}/{week}` stat lines for every synced week scored with the league's `scoring_settings`; optional `LineupPlayer.projected_points` and server-computed `MatchupLineup.projected_score` in `GetMatchupDetail`; and their Matchups display.

The player info card is authorized: a source-agnostic `playerprofile.Source` interface with an ESPN implementation over public athlete and fantasy endpoints (no cookies), fetched live each time a card opens rather than during sync and saved to a constrained `player_profiles` table (with `player_profile_stats` rows) as the fallback when a fetch fails, served by a `GetPlayerCard` RPC, and shown in a hover/tap card.

Checkpoint 12 Docker installation is authorized: a multi-stage Dockerfile with the frontend embedded behind the `embedweb` build tag, the API under `/api`, a localhost-only Host check, opt-in startup migrations (`MIGRATE_ON_START`), and a Compose `app` service published on `127.0.0.1` at `http://league-lens.localhost`. Image publication remains unapproved.

Checkpoint 12 Step 2 connect backend is authorized: encrypted ESPN credentials, ESPN and Sleeper league discovery, the owner-scoped `ConnectionService`, league detach/reattach, and sync from stored ESPN credentials.

Checkpoint 12 Step 3 connection UI is authorized: the shared connect dialog with ESPN bookmarklet/paste and Sleeper username, the `/connect` bookmarklet page, Settings → Leagues, the Home empty state, and the ESPN Reconnect banner.

## Code organization and readability

- Prefer focused, modular functions that each do one recognizable thing. A helper with only four lines is worthwhile when its name expresses a meaningful operation; reuse and line count are not prerequisites for extraction.
- Let calling code read as a sequence of clearly named operations. Use descriptive result variables so readers can understand what was computed or what happened without opening every helper.
- Keep orchestration separate from the details of fetching, validation, transformation, and persistence. Extract cohesive operations rather than arbitrary chunks of lines, and avoid vague names such as `handleData` or `processStuff`.
- Apply the same approach to frontend components and hooks: give each a clear responsibility and a name that describes its purpose. Small components are welcome when they represent a meaningful part of the interface or behavior.
- Prefer straightforward composition over unnecessary abstraction. The goal is readable responsibilities and clear data flow, not the maximum number of functions or files.

- Leave a blank line between function declarations (including small helpers) and separate logical blocks where it improves scanning. Keep a function's doc comment directly above its declaration.
- Document important business-logic functions with concise GoDoc-style comments (or the language equivalent). Explain purpose, business rules, assumptions, and meaningful edge cases rather than restating the code. Trivial helpers do not need boilerplate comments.
- Apply readability changes to handwritten code; do not hand-edit generated bindings.
