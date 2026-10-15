# Installation and in-app league connections

Status: proposed. Nothing in this document is implemented yet.

League Lens stays a laptop-only, self-hosted app. Installation remains
terminal-based through Docker; connecting ESPN and Sleeper leagues moves into the
browser. Hosting League Lens for other people, a native desktop installer, and
public exposure are out of scope.

## Installation

The user installs Docker Desktop (Windows or macOS) or OrbStack (macOS), then runs
`docker compose up -d` from a checkout and opens `http://league-lens.localhost`.

- A multi-stage `Dockerfile` builds the frontend, embeds it in the Go server, and
  ships a small runtime image. `.dockerignore` keeps environment files and keys
  out of the build context.
- The server serves the built frontend with a fallback to `index.html` for client
  routes. Connect and health routes take precedence.
- The server applies the embedded Goose migrations on startup before listening.
- `compose.yaml` gains an `app` service that waits for a healthy Postgres and
  publishes `127.0.0.1:${LEAGUE_LENS_PORT:-80}`. If port 80 is already taken,
  `LEAGUE_LENS_PORT=8080` in `.env` selects another port. The service sits behind
  a Compose profile so `just db-up` and `just dev` keep starting Postgres only.
- Names ending in `.localhost` resolve to the loopback address in Chrome, Edge,
  and Firefox without a hosts-file entry. Safari support is unverified.
- Requests whose `Host` is not `localhost`, `127.0.0.1`, or a `*.localhost` name
  are rejected, which blocks DNS rebinding.

## Connecting leagues

One connect flow is reused from Settings, the empty Home page, and the sync
failure banner.

- **Sleeper:** the user enters a username; the server looks up the user and lists
  their leagues for the season. No credentials are needed.
- **ESPN:** the server lists the account's leagues and team IDs from ESPN's fan
  API using the `SWID`, so the user never types a league ID. The cookies arrive by:
  - **Bookmarklet (primary):** clicked on espn.com in the user's normal browser.
    It reads `espn_s2` and `SWID` (neither is HttpOnly), URL-decodes them, and
    opens `/connect#s2=…&swid=…`. The page clears the fragment immediately with
    `history.replaceState` and posts the values to the server. A throwaway spike
    verified this end to end in Chrome.
  - **Paste (fallback):** the two cookie values copied from browser DevTools.
- The user picks leagues from a checklist; the server imports them through the
  shared importer and starts a sync.
- Settings lists each account with its leagues, status, and Reconnect, Remove,
  and Add actions. Removing a league detaches it: it stops syncing and is hidden,
  and its saved data is kept.
- A rejected ESPN cookie marks the connection `needs_reconnect`; the failure
  banner offers Reconnect.
- A server-opened ESPN login window (chromedp) also worked in a spike but needs
  Chrome on the same machine as the server, so it is deferred.

## Credential storage

ESPN cookies are authenticated sessions. ESPN offers no read-only token, so a
leaked `espn_s2` can act as the user in ESPN fantasy.

- Cookies are encrypted with AES-256-GCM from the Go standard library and stored
  in the existing `connections.encrypted_credentials` column. The connection ID
  is authenticated data, so a ciphertext copied to another row fails to decrypt.
- The 32-byte key is generated on first start into a file readable only by the
  app, in a separate app-data volume. It is never stored in Postgres, Git, or the
  image. `LEAGUE_LENS_SECRET_KEY` overrides the file.
- A lost key loses only the saved cookies: league data remains and ESPN
  connections show Reconnect. Key backup and rotation are not required for v1.
- No endpoint returns stored credentials, and they never appear in logs or errors.
- Sync reads each league's ESPN connection credentials and falls back to
  `ESPN_S2`/`ESPN_SWID` so current setups keep working.

This protects database dumps, backups, and copied volumes. It does not protect
against malware running as the user, which could read the key file but could
equally read the cookies from the browser's own cookie store.

## Delivery order

Each step is a separate pull request, approved before the next begins:

1. Docker installation: image, embedded frontend, startup migrations, the `app`
   Compose service, the Host check, and installation docs (checkpoint 12).
2. Connection backend: credential encryption, league discovery,
   `ConnectionService` (`ConnectESPN`, `ConnectSleeper`, `ListConnections`,
   `ImportLeagues`, `RemoveLeague`), and sync using stored credentials
   (checkpoints 5, 6, and 7).
3. Connection UI: the connect dialog, `/connect` bookmarklet page, Settings
   leagues section, Home empty state, and banner Reconnect (checkpoints 6, 7, and 8).
