# Repository instructions

- Work on a feature branch before editing files. Never commit or push directly to `main`.
- Push feature branches and use pull requests. Do not merge without explicit user approval.
- Follow the README checkpoint workflow: complete only the authorized checkpoint, report verification, and pause for review.
- Follow CONTRIBUTING.md, including Conventional Commit subjects.
- Never print, commit, or include local credentials in fixtures or build contexts. `.env.local` is private.

## Current scaffold constraints

- Keep scaffold changes local. Do not push branches or create PRs unless the user changes this instruction.
- sqlc generation is authorized for database queries via `just gen-db` (v1.30.0). Buf, go generate, and scaffold generators still require explicit authorization.
- Current implementation scope includes the Go/Postgres foundation and provider-independent import contracts with the Sleeper and ESPN adapters/commands. Frontend, Connect endpoints, background scheduling, and AI are deferred.

## Code organization and readability

- Prefer focused, modular functions that each do one recognizable thing. A helper with only four lines is worthwhile when its name expresses a meaningful operation; reuse and line count are not prerequisites for extraction.
- Let calling code read as a sequence of clearly named operations. Use descriptive result variables so readers can understand what was computed or what happened without opening every helper.
- Keep orchestration separate from the details of fetching, validation, transformation, and persistence. Extract cohesive operations rather than arbitrary chunks of lines, and avoid vague names such as `handleData` or `processStuff`.
- Apply the same approach to frontend components and hooks: give each a clear responsibility and a name that describes its purpose. Small components are welcome when they represent a meaningful part of the interface or behavior.
- Prefer straightforward composition over unnecessary abstraction. The goal is readable responsibilities and clear data flow, not the maximum number of functions or files.
