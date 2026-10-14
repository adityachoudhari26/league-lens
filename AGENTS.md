# Repository instructions

- Keep only one active development branch and PR at a time. Finish and merge it before starting the next. Do not create additional worktrees without asking.
- Work on a feature branch before editing files. Never commit or push directly to `main`.
- Push feature branches and use pull requests. Do not merge without explicit user approval.
- Follow the README checkpoint workflow: complete only the authorized checkpoint, report verification, and pause for review.
- Follow CONTRIBUTING.md, including Conventional Commit subjects.
- Never print, commit, or include local credentials in fixtures or build contexts. `.env.local` is private.

## Current scope

- sqlc generation is authorized for database queries via `just gen-db` (v1.30.0). Frontend scaffolding and shadcn component generation are authorized for the shell. Buf generation is authorized for dashboard contracts via `just gen-api`. Other Go/scaffold generators still require explicit authorization.
- Current implementation scope includes the Go/Postgres foundation and provider-independent import contracts with the Sleeper and ESPN adapters/commands. The Vite/React frontend shell and shadcn components are authorized. Owner-scoped Connect dashboard and overview endpoints are authorized. Frontend overview integration is authorized in its separate PR, including TypeScript contract generation via `just gen-web`. Checkpoint 9 provider synchronization is authorized: server-side sync through the shared importer, stale refresh on dashboard load, and an explicit Refresh that contacts providers. River-backed scheduling belongs to a later Checkpoint 9 PR. AI remains deferred.

## Code organization and readability

- Prefer focused, modular functions that each do one recognizable thing. A helper with only four lines is worthwhile when its name expresses a meaningful operation; reuse and line count are not prerequisites for extraction.
- Let calling code read as a sequence of clearly named operations. Use descriptive result variables so readers can understand what was computed or what happened without opening every helper.
- Keep orchestration separate from the details of fetching, validation, transformation, and persistence. Extract cohesive operations rather than arbitrary chunks of lines, and avoid vague names such as `handleData` or `processStuff`.
- Apply the same approach to frontend components and hooks: give each a clear responsibility and a name that describes its purpose. Small components are welcome when they represent a meaningful part of the interface or behavior.
- Prefer straightforward composition over unnecessary abstraction. The goal is readable responsibilities and clear data flow, not the maximum number of functions or files.

- Leave a blank line between function declarations (including small helpers) and separate logical blocks where it improves scanning. Keep a function's doc comment directly above its declaration.
- Document important business-logic functions with concise GoDoc-style comments (or the language equivalent). Explain purpose, business rules, assumptions, and meaningful edge cases rather than restating the code. Trivial helpers do not need boilerplate comments.
- Apply readability changes to handwritten code; do not hand-edit generated bindings.
