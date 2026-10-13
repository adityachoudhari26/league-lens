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

# Build the API and migration binaries.
build:
    mkdir -p bin
    go build -o bin/server ./cmd/server
    go build -o bin/migrate ./cmd/migrate

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
