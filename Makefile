.PHONY: help db-up db-stop migrate-up migrate-down serve test test-integration build check fmt

help:
	@printf '%s\n' 'db-up / db-stop: local Postgres' 'migrate-up / migrate-down: explicit migrations (DATABASE_URL required)' 'serve: Go health server (DATABASE_URL required)' 'test / test-integration: tests (integration needs TEST_DATABASE_URL)' 'build / check / fmt: local checks; no code generation'

db-up:
	docker compose up -d --wait postgres

db-stop:
	docker compose stop postgres

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

serve:
	go run ./cmd/server

test:
	go test ./...

test-integration:
	go test -tags=integration -count=1 ./db

build:
	mkdir -p bin
	go build -o bin/server ./cmd/server
	go build -o bin/migrate ./cmd/migrate

check:
	go vet ./...
	git diff --check
	@test -z "$$(gofmt -l cmd internal db)" || (echo 'Run make fmt'; exit 1)

fmt:
	gofmt -w cmd internal db
