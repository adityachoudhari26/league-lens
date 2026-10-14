set dotenv-load := true
set dotenv-filename := ".env.development"
set dotenv-override := false

# Show available development commands.
default:
    @just --list

# Create local development config without overwriting existing values.
setup:
    @if [ -e .env.development ]; then echo '.env.development already exists; left unchanged'; else (umask 077; set -C; cat .env.example > .env.development) && echo 'Created .env.development'; fi

# Install the pinned Air version into the ignored project bin directory.
tools:
    GOBIN="{{justfile_directory()}}/bin" go install github.com/air-verse/air@v1.65.3

# Explicitly regenerate database bindings with the version used by this repository.
gen-db:
    @version="$(sqlc version)"; test "$version" = "v1.30.0" || { echo 'sqlc v1.30.0 is required; see README installation instructions.' >&2; exit 1; }
    sqlc generate

# Start local Postgres and wait for it to become healthy.
db-up:
    docker compose up -d --wait postgres

# Stop Postgres while retaining its data volume.
db-stop:
    docker compose stop postgres

# Apply pending migrations explicitly.
migrate: _require-env
    go run ./cmd/migrate up

# Roll back one migration (destructive; prompts for confirmation).
[confirm('Roll back one migration? This can delete application data.')]
migrate-down: _require-env
    go run ./cmd/migrate down

# Rebuild and restart the Go API on source changes; no migrations or generation.
dev-api: _require-env _require-air
    ./bin/air -c .air.toml

# Run the API once, without watching files.
serve: _require-env
    go run ./cmd/server

# Run Go unit tests.
test:
    go test ./...

# Run integration tests in a disposable Postgres container; clean it up on exit.
test-db:
    ./scripts/test-db.sh

# Build the API, migration, and import binaries.
build:
    mkdir -p bin
    go build -o bin/server ./cmd/server
    go build -o bin/migrate ./cmd/migrate
    go build -o bin/import ./cmd/import

# Check Go, shell syntax, formatting, and whitespace without changing files.
check:
    go vet ./...
    sh -n scripts/test-db.sh
    git diff --check
    @test -z "$(gofmt -l cmd internal db)" || (echo 'Run just fmt'; exit 1)

# Format Go source files.
fmt:
    gofmt -w cmd internal db

[private]
_require-env:
    @test -n "${DATABASE_URL:-}" || (echo 'DATABASE_URL is required. Run just setup or export it.' >&2; exit 1)

[private]
_require-air:
    @test -x ./bin/air || (echo 'Project Air is missing. Run just tools.' >&2; exit 1)

# Import a public Sleeper league for the local owner (not authentication).
import-sleeper username league_id owner_email="owner@localhost": _require-env
    go run ./cmd/import sleeper --username {{quote(username)}} --league-id {{quote(league_id)}} --owner-email {{quote(owner_email)}}

# Import a private ESPN league; export ESPN_S2 and ESPN_SWID first.
import-espn league_id season team_id owner_email="owner@localhost": _require-env
    go run ./cmd/import espn --league-id {{quote(league_id)}} --season {{quote(season)}} --team-id {{quote(team_id)}} --owner-email {{quote(owner_email)}}

# Install frontend dependencies. Requires Node 22.12+ and pnpm 11.15.1.
setup-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web install --frozen-lockfile

# Run the frontend shell; no database or API is needed yet.
dev-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web dev

# Lint and type-check the frontend.
check-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web check

# Build the frontend to web/dist.
build-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web build


# Run the frontend and Go API together; Ctrl+C stops both.
dev: _require-env _require-air
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web dev:all

# Lint frontend source without changing files.
lint-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web lint

# Apply safe automatic frontend lint fixes.
fix-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web lint:fix

# Format frontend source with Prettier.
fmt-web:
    PATH="{{justfile_directory()}}/bin:$PATH" pnpm --dir web format

# Regenerate Go protobuf and Connect bindings.
gen-api:
    go run github.com/bufbuild/buf/cmd/buf@v1.61.0 lint
    go run github.com/bufbuild/buf/cmd/buf@v1.61.0 generate

# Regenerate TypeScript API contracts after just setup-web.
gen-web:
    PATH="{{justfile_directory()}}/bin:$PATH" go run github.com/bufbuild/buf/cmd/buf@v1.61.0 generate --template buf.gen.web.yaml
