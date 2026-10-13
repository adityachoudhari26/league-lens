#!/bin/sh
# Isolated test database: never use DATABASE_URL or the development Compose volume.
set -eu
cd "$(dirname "$0")/.."
container=''
cleanup() {
  if [ -n "$container" ]; then docker rm -f "$container" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
container=$(docker run --rm -d \
  -e POSTGRES_USER=league_lens_test \
  -e POSTGRES_PASSWORD=local_test_only \
  -e POSTGRES_DB=league_lens_test \
  -p 127.0.0.1::5432 postgres:18.3-alpine)
attempt=0
until docker exec "$container" pg_isready -h 127.0.0.1 -U league_lens_test -d league_lens_test >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then echo 'Test Postgres did not become ready' >&2; exit 1; fi
  sleep 1
done
binding=$(docker port "$container" 5432/tcp)
port=${binding##*:}
TEST_DATABASE_URL="postgres://league_lens_test:local_test_only@127.0.0.1:$port/league_lens_test?sslmode=disable" \
  go test -tags=integration -count=1 ./...
