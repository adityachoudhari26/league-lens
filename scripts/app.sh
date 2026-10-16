#!/usr/bin/env bash
set -euo pipefail

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    printf "%s\n" "Docker isn't installed. Install Docker Desktop (https://www.docker.com/products/docker-desktop/) or OrbStack (https://orbstack.dev/), open it, and run this again." >&2
    return 1
  fi

  if ! docker info >/dev/null 2>&1; then
    printf "%s\n" "Docker isn't running. Open Docker Desktop (or OrbStack), wait until it says it's running, and run this again." >&2
    return 1
  fi

  if ! docker compose version >/dev/null 2>&1; then
    printf "%s\n" "The Docker Compose plugin is needed. Update Docker Desktop or install the Docker Compose plugin, then run this again." >&2
    return 1
  fi
}

dotenv_has_port() {
  [[ -f .env ]] && awk -F= '$1 ~ /^[[:space:]]*LEAGUE_LENS_PORT[[:space:]]*$/ { found = 1 } END { exit !found }' .env
}

dotenv_port() {
  [[ -f .env ]] || return 0

  local line value
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*LEAGUE_LENS_PORT[[:space:]]*=(.*)$ ]] || continue
    value=${BASH_REMATCH[1]%%#*}
    value=${value//[[:space:]]/}
    value=${value//\"/}
    value=${value//\'/}
    printf '%s\n' "$value"
  done < .env | tail -n 1
}

app_port() {
  local published_port configured_port
  published_port=$(docker compose port app 8080 2>/dev/null | tail -n 1 || true)
  if [[ -n "$published_port" ]]; then
    printf '%s\n' "${published_port##*:}"
    return
  fi

  if [[ -n "${LEAGUE_LENS_PORT:-}" ]]; then
    printf '%s\n' "$LEAGUE_LENS_PORT"
    return
  fi

  configured_port=$(dotenv_port)
  printf '%s\n' "${configured_port:-80}"
}

app_urls() {
  local port suffix
  port=$(app_port)
  suffix=''
  [[ "$port" == 80 ]] || suffix=":$port"

  APP_URL="http://league-lens.localhost$suffix"
  SAFARI_URL="http://localhost$suffix"
}

local_app_url() {
  local port suffix
  port=$(app_port)
  suffix=''
  [[ "$port" == 80 ]] || suffix=":$port"
  printf 'http://localhost%s\n' "$suffix"
}

wait_until_ready() {
  local port attempt
  port=$(app_port)

  for ((attempt = 0; attempt < 60; attempt++)); do
    if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done

  printf "%s\n" "League Lens didn't start. Run \`just logs\` to see why." >&2
  return 1
}

is_app_running() {
  local running_services service
  running_services=$(docker compose ps --status running --services 2>/dev/null || true)
  while IFS= read -r service; do
    [[ "$service" == app ]] && return 0
  done <<< "$running_services"
  return 1
}

print_app_urls() {
  app_urls
  printf 'League Lens is running at %s (Safari: %s)\n' "$APP_URL" "$SAFARI_URL"
}

launch_browser() {
  local url=$1

  case "$(uname -s)" in
    Darwin)
      if command -v open >/dev/null 2>&1; then
        open "$url" >/dev/null 2>&1
        return
      fi
      ;;
    Linux)
      if command -v xdg-open >/dev/null 2>&1; then
        xdg-open "$url" >/dev/null 2>&1 &
        return
      fi
      ;;
  esac

  printf 'Open this address in your browser: %s\n' "$url"
}

run_compose_build() {
  local output_file
  output_file=$(mktemp)

  if docker compose up -d --build 2>&1 | tee "$output_file"; then
    rm -f "$output_file"
    return 0
  fi

  if grep -Eiq 'port is already allocated|address already in use' "$output_file"; then
    rm -f "$output_file"
    return 2
  fi

  rm -f "$output_file"
  return 1
}

port_override_is_set() {
  [[ "${LEAGUE_LENS_PORT+x}" == x ]] || dotenv_has_port
}

use_fallback_port() {
  if [[ ! -e .env ]]; then
    (umask 077; : > .env)
  fi
  if [[ -s .env && -n "$(tail -c 1 .env)" ]]; then
    printf '\n' >> .env
  fi
  printf 'LEAGUE_LENS_PORT=8080\n' >> .env
}

print_port_conflict_help() {
  printf "%s\n" "That port is already in use. Set a different LEAGUE_LENS_PORT in .env, then run \`just install\` again." >&2
}

# Install the app and automatically choose port 8080 if the default port is busy.
install_app() {
  require_docker
  printf '%s\n' "Building and starting League Lens. The first run takes a few minutes…"

  if run_compose_build; then
    :
  else
    local build_result=$?
    if [[ "$build_result" -ne 2 ]]; then
      return 1
    fi

    if port_override_is_set; then
      print_port_conflict_help
      return 1
    fi

    use_fallback_port
    printf '%s\n' "Port 80 is busy, so League Lens will use port 8080 instead."
    if run_compose_build; then
      :
    else
      local retry_result=$?
      if [[ "$retry_result" -eq 2 ]]; then
        print_port_conflict_help
      fi
      return 1
    fi
  fi

  wait_until_ready
  app_urls
  launch_browser "$(local_app_url)"
  printf '\nLeague Lens is running at %s (Safari: %s)\n' "$APP_URL" "$SAFARI_URL"
  printf '%s\n' "Next: click Connect leagues."
  printf '\nEveryday commands:\n'
  printf '  just start    Start League Lens\n'
  printf '  just stop     Stop it without deleting your data\n'
  printf '  just update   Install the latest version\n'
  printf '  just status   See whether it is running\n'
  printf '  just logs     See recent app activity\n'
}

start_app() {
  require_docker
  docker compose up -d --build
  wait_until_ready
  print_app_urls
}

stop_app() {
  require_docker
  docker compose stop
  printf '%s\n' "Stopped. Your leagues and data are kept. It won't start again automatically until you run \`just start\`."
}

restart_app() {
  require_docker
  if is_app_running; then
    docker compose restart app
  else
    docker compose up -d --build
  fi
  wait_until_ready
  print_app_urls
}

open_app() {
  require_docker
  if ! is_app_running; then
    printf "%s\n" "League Lens isn't running. Run \`just start\` first." >&2
    return 1
  fi

  launch_browser "$(local_app_url)"
}

installed_version() {
  local revision commit_date
  if ! revision=$(git rev-parse --short HEAD 2>/dev/null); then
    printf '%s\n' "unknown (not a git download)"
    return
  fi

  commit_date=$(git log -1 --format=%cd --date=short 2>/dev/null || true)
  if [[ -n "$commit_date" ]]; then
    printf '%s (%s)\n' "$revision" "$commit_date"
  else
    printf '%s\n' "$revision"
  fi
}

status_app() {
  require_docker
  if is_app_running; then
    printf '%s\n' "League Lens is running."
    print_app_urls
  else
    printf '%s\n' "League Lens is stopped."
  fi
  printf 'Installed version: %s\n' "$(installed_version)"
}

logs_app() {
  require_docker
  docker compose logs -f --tail=100 app
}

db_shell() {
  require_docker

  if [[ -t 0 ]]; then
    docker compose exec postgres psql -U league_lens -d league_lens "$@"
  else
    docker compose exec -T postgres psql -U league_lens -d league_lens "$@"
  fi
}

tracked_changes() {
  git status --short --untracked-files=no
}

# Update only a clean main checkout, then rebuild and restart if it was running.
update_app() {
  if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    printf '%s\n' "This copy was downloaded as a ZIP, so it can't update itself. Download the new ZIP from GitHub, or reinstall with git (see README)." >&2
    return 1
  fi

  require_docker

  local branch old_revision new_revision was_running
  branch=$(git branch --show-current 2>/dev/null || true)
  if [[ "$branch" != main ]]; then
    branch=${branch:-"detached HEAD"}
    printf "You're on branch %s. Switch back with \`git switch main\`, then run \`just update\` again.\n" "$branch" >&2
    return 1
  fi

  if ! git diff --quiet || ! git diff --cached --quiet; then
    printf '%s\n' "You have local edits to League Lens files, so updating could overwrite them:" >&2
    tracked_changes >&2
    return 1
  fi

  old_revision=$(git rev-parse HEAD)
  local pull_output
  if ! pull_output=$(git pull --ff-only 2>&1); then
    printf '%s\n' "League Lens couldn't update automatically. Git said:" >&2
    printf '%s\n' "$pull_output" >&2
    return 1
  fi
  new_revision=$(git rev-parse HEAD)

  if [[ "$old_revision" == "$new_revision" ]]; then
    printf '%s\n' "Already up to date."
    return 0
  fi

  printf '%s\n' "What's new:"
  git log --no-merges --format='  - %s' "$old_revision..$new_revision"

  if is_app_running; then
    was_running=yes
  else
    was_running=no
  fi

  docker compose build app
  if [[ "$was_running" == yes ]]; then
    docker compose up -d
    wait_until_ready
    app_urls
    printf 'Updated and restarted. League Lens is running at %s\n' "$APP_URL"
  else
    printf '%s\n' "Updated. Run \`just start\` to start League Lens."
  fi
}

command_name=${1:-}
if [[ $# -gt 0 ]]; then
  shift
fi

case "$command_name" in
  install) install_app ;;
  start) start_app ;;
  stop) stop_app ;;
  restart) restart_app ;;
  open) open_app ;;
  status) status_app ;;
  logs) logs_app ;;
  db-shell) db_shell "$@" ;;
  update) update_app ;;
  *)
    printf 'Unknown app command: %s\n' "${command_name:-<none>}" >&2
    printf '%s\n' "Use: install, start, stop, restart, open, status, logs, db-shell, or update." >&2
    exit 2
    ;;
esac
