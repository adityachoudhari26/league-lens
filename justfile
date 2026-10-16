set dotenv-load := true
set dotenv-filename := ".env.development"
set dotenv-override := false

# Show available development commands.
[group('dev')]
default:
    @just --list

# Install League Lens, then open it in your browser.
[group('app')]
install:
    @scripts/app.sh install

# Start League Lens and rebuild if needed.
[group('app')]
start:
    @scripts/app.sh start

# Stop League Lens without deleting its data.
[group('app')]
stop:
    @scripts/app.sh stop

# Restart League Lens.
[group('app')]
restart:
    @scripts/app.sh restart

# Open League Lens in your browser.
[group('app')]
open:
    @scripts/app.sh open

# Show whether League Lens is running and its installed version.
[group('app')]
status:
    @scripts/app.sh status

# Follow recent League Lens activity.
[group('app')]
logs:
    @scripts/app.sh logs

# Open a database prompt or run a database command.
[group('app')]
[positional-arguments]
db-shell *args:
    @scripts/app.sh db-shell "$@"

# Update League Lens from its GitHub repository.
[group('app')]
update:
    @scripts/app.sh update

# Create local development config without overwriting existing values.
[group('dev')]
setup:
    @if [ -e .env.development ]; then echo '.env.development already exists; left unchanged'; else (umask 077; set -C; cat .env.example > .env.development) && echo 'Created .env.development'; fi

# Install the pinned Air version into the ignored project bin directory.
[group('dev')]
tools:
    GOBIN="{{justfile_directory()}}/bin" go install github.com/air-verse/air@v1.65.3

# Explicitly regenerate database bindings with the version used by this repository.
[group('dev')]
gen-db:
    @version="$(sqlc version)"; test "$version" = "v1.30.0" || { echo 'sqlc v1.30.0 is required; see README installation instructions.' >&2; exit 1; }
    sqlc generate

# Start local Postgres and wait for it to become healthy.
[group('dev')]
db-up:
    docker compose up -d --wait postgres

# Stop Postgres while retaining its data volume.
[group('dev')]
db-stop:
    docker compose stop postgres

# Apply pending migrations explicitly.
[group('dev')]
migrate: _require-env
    go run ./cmd/migrate up

# Roll back one migration (destructive; prompts for confirmation).
[confirm('Roll back one migration? This can delete application data.')]
[group('dev')]
migrate-down: _require-env
    go run ./cmd/migrate down

# Rebuild and restart the Go API on source changes; no migrations or generation.
[group('dev')]
dev-api: _require-env _require-air
    ./bin/air -c .air.toml

# Run the API once, without watching files.
[group('dev')]
serve: _require-env
    go run ./cmd/server

# Run Go unit tests.
[group('dev')]
test:
    go test ./...

# Run integration tests in a disposable Postgres container; clean it up on exit.
[group('dev')]
test-db:
    ./scripts/test-db.sh

# Build the API, migration, and import binaries.
[group('dev')]
build:
    mkdir -p bin
    go build -o bin/server ./cmd/server
    go build -o bin/migrate ./cmd/migrate
    go build -o bin/import ./cmd/import

# Check Go, shell syntax, formatting, lint, and whitespace without changing files.
[group('dev')]
check: lint
    go vet ./...
    sh -n scripts/test-db.sh
    bash -n scripts/app.sh
    git diff --check
    @test -z "$(gofmt -l cmd internal db)" || (echo 'Run just fmt'; exit 1)

# Run golangci-lint (linters and import/format checks) with the pinned version.
[group('dev')]
lint:
    go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run

# Format Go source files.
[group('dev')]
fmt:
    gofmt -w cmd internal db

[group('dev')]
[private]
_require-env:
    @test -n "${DATABASE_URL:-}" || (echo 'DATABASE_URL is required. Run just setup or export it.' >&2; exit 1)

[group('dev')]
[private]
_require-air:
    @test -x ./bin/air || (echo 'Project Air is missing. Run just tools.' >&2; exit 1)

# Import a public Sleeper league for the local owner (not authentication).
[group('dev')]
import-sleeper username league_id owner_email="owner@localhost": _require-env
    go run ./cmd/import sleeper --username {{quote(username)}} --league-id {{quote(league_id)}} --owner-email {{quote(owner_email)}}

# Import a private ESPN league; export ESPN_S2 and ESPN_SWID first.
[group('dev')]
import-espn league_id season team_id owner_email="owner@localhost": _require-env
    go run ./cmd/import espn --league-id {{quote(league_id)}} --season {{quote(season)}} --team-id {{quote(team_id)}} --owner-email {{quote(owner_email)}}

# Install frontend dependencies. Requires Node 22.12+ and pnpm 11.15.1.
[group('dev')]
setup-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web install --frozen-lockfile

# Run the frontend shell; no database or API is needed yet.
[group('dev')]
dev-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web dev

# Lint and type-check the frontend.
[group('dev')]
check-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web check

# Build the frontend to web/dist.
[group('dev')]
build-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web build


# Run the frontend and Go API together; Ctrl+C stops both.
[group('dev')]
dev: _require-env _require-air
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web dev:all

# Lint frontend source without changing files.
[group('dev')]
lint-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web lint

# Apply safe automatic frontend lint fixes.
[group('dev')]
fix-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web lint:fix

# Format frontend source with Prettier.
[group('dev')]
fmt-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web format

# Regenerate Go protobuf and Connect bindings.
[group('dev')]
gen-api:
    go run github.com/bufbuild/buf/cmd/buf@v1.61.0 lint
    go run github.com/bufbuild/buf/cmd/buf@v1.61.0 generate

# Regenerate TypeScript API contracts after just setup-web.
[group('dev')]
gen-web:
    PATH="{{justfile_directory()}}/bin:$PATH" go run github.com/bufbuild/buf/cmd/buf@v1.61.0 generate --template buf.gen.web.yaml
