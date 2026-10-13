# League Lens

An open-source, self-hosted fantasy football dashboard for ESPN and Sleeper, with optional AI powered by your own credentials. The core dashboard should work without an AI subscription or mandatory paid data service. Yahoo is deferred.

## How we'll work

We will build one checkpoint at a time. After each checkpoint, stop, show what changed, report the checks performed and any limitations, and wait for your approval before starting the next checkpoint. Approval applies only to the next checkpoint, not the entire roadmap.

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

- [ ] **1. Review the scaffold design.** Agree on directory layout, local development commands, configuration, and the minimal frontend-to-backend health flow. Record the design and short implementation plan before scaffolding. **Done when:** you approve the concrete scaffold scope.

- [ ] **2. Create the project skeleton.** Add React/Vite and Go entry points, dependency manifests and lockfile, basic UI styling, configuration validation, and secret-free configuration examples. **Done when:** the frontend builds, the Go backend builds, and documented development commands work. No fantasy integration yet.

- [ ] **3. Connect the frontend to Go.** Define a small health service in Protobuf, configure reproducible code generation, and call it from the frontend through Connect Query. **Done when:** the page displays the real backend response and a useful error when the backend is unavailable; generated bindings reproduce cleanly.

- [ ] **4. Add Postgres and migrations.** Add a local database service, Goose migrations, pgx connections, and sqlc queries. Design ownership for users, connections, leagues, and selected teams. Separate process health from database readiness. **Done when:** an empty database migrates successfully, a database round trip works, and an unavailable database is reported accurately.

- [ ] **5. Add owner setup and credential protection.** Review and implement owner authentication, protected setup, server-side sessions, and encrypted credential storage. Keep registration closed by default. **Done when:** unauthorized requests cannot access protected data, secrets do not appear in responses or logs, and setup cannot be claimed without its token. Document encryption-key backup requirements.

- [ ] **6. Build the ESPN connector.** Turn the verified read-only flow into reusable Go code; normalize league settings, teams, roster slots, players, and matchups. Add UI configuration and reconnect/error states. **Done when:** the selected ESPN team imports correctly, fixture tests verify normalization, and an expired or invalid connection leaves the last successful snapshot intact.

- [ ] **7. Build the Sleeper connector.** Look up a username, select a league/team, and normalize the same data categories. Treat public username lookup as selection, not proof of account ownership. **Done when:** the selected Sleeper team imports correctly and fixture tests cover its mappings and missing-data cases.

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
db/                   SQL queries, migrations, and generated query code
docs/                 Approved designs and implementation notes
compose.yaml          Self-hosted services
Dockerfile            Frontend build and Go runtime packaging
```

## Current state

Only Git initialization, MIT licensing, local connection configuration, manual provider checks, and this checklist are complete. Application code, dependency installation, database setup, and deployment have not started.

Credentials live in the ignored `.env.local` file. Never commit that file, paste its contents into chat, or include it in a Docker build context. Add appropriate Docker exclusions when packaging begins. Examples and test fixtures must contain no real credentials or private league data.

**Next:** review checkpoint 1 together. Do not begin scaffolding until it is approved.

## License

This project is licensed under the [MIT License](LICENSE). You may use, modify, redistribute, and commercially host the software subject to its license terms. External services and data remain subject to their own terms.
