# Contributing to League Lens

Follow the checkpoints in [README.md](README.md). Keep each change focused and explain what changed and how it was verified.

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
- Format Go with `gofmt` once Go files exist. Frontend formatting and lint tools will be pinned with the scaffold.
- Commit dependency lockfiles, SQL migrations, and generated API/query bindings when introduced. Dependency directories and build artifacts are ignored.
- Configuration examples must contain placeholders, never real tokens, cookies, or private league data. `.env.local` remains private; all environment files are excluded from Docker build contexts.
- Ignore rules do not protect files already tracked by Git. Review staged changes before committing; never force-add credentials.

## Before a commit

```sh
git diff --cached --check
git diff --cached --stat
```

Run the checks relevant to your changes and report the results. The scaffold will add reproducible build, lint, generation, and test commands; none are claimed to exist yet. Do not rewrite published history just to convert older commits to the new convention.
