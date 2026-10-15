# Contributing to League Lens

Follow the checkpoints in [docs/roadmap.md](docs/roadmap.md). See [docs/development.md](docs/development.md) for running from source. Keep each change focused and explain what changed and how it was verified.

## Branches and pull requests

Create a feature branch before making changes (for example, `chore/repo-hygiene` or `feat/app-scaffold`). Push that branch and open a pull request for review. Never commit or push directly to `main`. Do not merge a pull request without the owner's approval.

The local pre-commit hook rejects commits on `main` and `master`. Hooks must be enabled on each clone; GitHub branch protection is separate and is not configured by this repository.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```text
<type>[optional scope][!]: <description>
```

Examples:

```text
feat(espn): import league rosters
fix(sleeper): handle missing matchup
chore(repo): configure repository defaults
```

Accepted types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, and `revert`. Scope is optional; use a short module name such as `web`, `api`, `espn`, or `repo`. Use `!` and an explanatory `BREAKING CHANGE:` footer when applicable. Git-generated merge and revert subjects are allowed. Write informative subjects; the hook checks their structure, not their meaning or breaking-change accuracy.

Enable the versioned hook after cloning:

```sh
git config --local core.hooksPath .githooks
```

This sets the hook path for this repository only. If you already use a custom hook manager, integrate `scripts/check-commit-message.sh` there instead of replacing your setup. Local hooks are opt-in on each clone and are not server-side enforcement. No Node, Husky, or dependency installation is needed.

## Files and formatting

- Follow `.editorconfig` and `.gitattributes` for UTF-8, LF line endings, and indentation.
- Format Go with `gofmt` once Go files exist. Frontend code is formatted with Prettier; run `just fmt-web` to apply it, and `just check-web` for lint, the format check, and type checks.
- Commit dependency lockfiles, SQL migrations, and generated API/query bindings when introduced. Dependency directories and build artifacts are ignored.
- Configuration examples must contain placeholders, never real tokens, cookies, or private league data. `.env.local` remains private; all environment files are excluded from Docker build contexts.
- Ignore rules do not protect files already tracked by Git. Review staged changes before committing; never force-add credentials.

## Before a commit

```sh
git diff --cached --check
git diff --cached --stat
```

Run the checks relevant to your changes and report the results. The scaffold will add reproducible build, lint, generation, and test commands; none are claimed to exist yet. Do not rewrite published history just to convert older commits to the new convention.

## Continuous integration

Pull requests and pushes to `main` run Go formatting, shell syntax, vet, race
checks, builds, disposable Postgres integration tests, and a sqlc v1.30.0 drift
check. Run `just check`, `go test -race ./...`, `just build`, `just test-db`, and
`just gen-db` locally before submitting; commit any regenerated files.

PR titles must follow the same Conventional Commit format as local commits
because they become squash commit subjects. Editing a title reruns its check.
Frontend changes are checked with ESLint, TypeScript, and a production build. Run `just check-web` and `just build-web` locally. Workflows use read-only repository permissions and need no provider credentials.

Recommended required checks in GitHub branch rules: `Go checks`, `Database
integration`, `Generated database code`, and `Conventional PR title`. Adding the
workflows does not enable branch protection automatically. Release publishing is
deferred until the app is ready to distribute.
