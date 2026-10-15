<p align="center">
  <img src="web/public/favicon.svg" alt="League Lens icon" width="128" height="128" />
</p>

<h1 align="center">League Lens</h1>

<p align="center">
  A self-hosted fantasy football dashboard for your ESPN and Sleeper leagues.
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License" /></a>
  <img src="https://img.shields.io/badge/status-early%20development-orange.svg" alt="Status: early development" />
</p>

League Lens pulls every team you manage, across ESPN and Sleeper, into one
dashboard that runs on your own computer. It shows what needs your attention
this week: matchups, lineup problems, injuries, byes, and player news. No
account, subscription, or paid data feed is required.

> [!WARNING]
> League Lens is in early development. It has no login yet and is meant to run
> only on your own machine (`localhost`). Do not expose it to the internet.

## Table of contents

- [Features](#features)
- [Roadmap](#roadmap)
- [Installation](#installation)
- [Connecting leagues](#connecting-leagues)
- [Updating, stopping, and removing](#updating-stopping-and-removing)
- [Configuration](#configuration)
- [Privacy and security](#privacy-and-security)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Features

**Leagues**

- ESPN (including private leagues) and Sleeper in one place.
- Imports full leagues: every team, roster, record, and the current season's
  weekly history.
- Players are matched exactly across providers using public ID mappings.
  Players are never merged by name alone.

**Overview**

- **Attention:** alerts for empty starting slots, injured starters (Out,
  Doubtful, and IR need action; Questionable is flagged to monitor), and
  starters on bye. Alerts disappear at kickoff and can be dismissed with Undo.
- **Matchups:** this week's score for each of your teams, with when each score
  was fetched.
- **Player updates:** recent player news (RotoWire blurbs from ESPN) for your
  rostered players, with injuries first. You can filter by league, player, or
  injuries.

**Matchups page**

- Starting lineups for both teams with each player's points and projection,
  scored with your league's rules, plus a projected total.
- Pick any league and week.

**Player cards**

- Hover or tap a player to see a card with team colors, bio, draft details,
  season stats with position rank, next game, roster percentage, and the latest
  fantasy outlook.

**Sync**

- Saved data shows immediately. Data older than 15 minutes refreshes in the
  background when you open the dashboard. **Refresh** syncs every league now.
- A league that fails to sync keeps showing its last saved data.

**App**

- Light, Dark, and System themes, and a sidebar that collapses on small screens.
- One command to install with Docker. Data is stored in Postgres on your
  machine.

## Roadmap

These features are planned but not built yet. The full checkpoint tracker is in
[docs/roadmap.md](docs/roadmap.md).

- [ ] **Connect leagues in the app:** set up ESPN and Sleeper in Settings
  instead of the command line, with a bookmarklet or pasted cookies for ESPN
  and a Reconnect flow when cookies expire. The backend for this is done.
- [ ] **Roster and Standings pages.**
- [ ] **Connection health:** show when a league's data is stale or its
  credentials need attention.
- [ ] **Scheduled sync:** keep data fresh without the browser open, using
  persistent background jobs with retries and rate limits.
- [ ] **More alerts:** checks for next week's lineup, and bench players
  projected to outscore starters.
- [ ] **Login and owner setup:** authentication and server-side sessions, so
  the app can be used safely off your own machine.
- [ ] **Backup and restore:** documented and tested.
- [ ] **Optional AI:** bring your own API key for sourced briefings and
  questions about your roster. The dashboard will always work without AI.
- [ ] **Releases:** prebuilt images and release automation.

Not planned yet: Yahoo, win-probability charts, future matchup schedules, and a
standalone CLI.

## Installation

### Requirements

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) on Windows
  or macOS, [OrbStack](https://orbstack.dev/) on macOS, or Docker Engine with
  the Compose plugin on Linux.
- Git, or a downloaded ZIP of this repository.

### Run with Docker

```sh
git clone https://github.com/adityachoudhari26/league-lens.git
cd league-lens
docker compose up -d
```

The first run builds the image on your machine, which can take a few minutes.
Database migrations run automatically on startup.

Open **http://league-lens.localhost** in Chrome, Edge, or Firefox. In Safari,
use **http://localhost**.

If port 80 is already in use, create a `.env` file next to `compose.yaml`:

```sh
LEAGUE_LENS_PORT=8080
```

Then run `docker compose up -d` again and open
`http://league-lens.localhost:8080` (Safari: `http://localhost:8080`).

## Connecting leagues

You connect leagues from the terminal for now. In-app setup is on the
[roadmap](#roadmap).

### Sleeper

Sleeper needs no credentials. Use your Sleeper username and the league ID from
the league URL (`sleeper.com/leagues/<league-id>`):

```sh
docker compose exec app import sleeper --username YOUR_USERNAME --league-id YOUR_LEAGUE_ID
```

### ESPN

ESPN private leagues need two cookies from a browser where you're signed in to
ESPN:

1. Open [espn.com](https://www.espn.com) and sign in.
2. Open your browser's developer tools, go to **Application** (Chrome/Edge) or
   **Storage** (Firefox), then **Cookies**, and copy the values of `espn_s2`
   and `SWID`.
3. Add them to the `.env` file next to `compose.yaml`:

   ```sh
   ESPN_S2=your-espn_s2-value
   ESPN_SWID={your-swid-value}
   ```

4. Restart the app so it picks up the cookies, then import your league. The
   league ID and team ID come from your team page URL
   (`fantasy.espn.com/football/team?leagueId=<league-id>&teamId=<team-id>`):

   ```sh
   docker compose up -d
   docker compose exec app import espn --league-id YOUR_LEAGUE_ID --season 2026 --team-id YOUR_TEAM_ID
   ```

The season is required because ESPN reuses league IDs across years.

## Updating, stopping, and removing

| Task | Command |
| --- | --- |
| Update to the latest version | `git pull && docker compose up -d --build` |
| Stop the app (keeps data) | `docker compose stop` |
| Start it again | `docker compose up -d` |
| Delete the app **and all saved data** | `docker compose down -v` |

## Configuration

Set these in a `.env` file next to `compose.yaml`. All are optional.

| Variable | Default | Purpose |
| --- | --- | --- |
| `LEAGUE_LENS_PORT` | `80` | Port the app listens on, on `127.0.0.1`. |
| `ESPN_S2`, `ESPN_SWID` | empty | ESPN cookies used for importing and syncing ESPN leagues. |
| `LOCAL_OWNER_EMAIL` | `owner@localhost` | Label for the local owner. This is not a login. |
| `POSTGRES_PASSWORD` | `league_lens_local` | Password for the bundled Postgres database. |

## Privacy and security

- Everything runs on your machine. The app and database listen only on
  `127.0.0.1`, and the server rejects requests that aren't addressed to
  `localhost`.
- League Lens only reads from ESPN and Sleeper. It never changes your lineups
  or league settings.
- ESPN cookies saved by the app are encrypted (AES-256-GCM) and are never shown
  or logged. If the key is lost, you only need to reconnect ESPN.
- There is no authentication yet. Don't expose the app beyond `localhost`
  until [login and owner setup](#roadmap) ship.
- ESPN's fantasy API is undocumented and may change without notice. Sleeper is
  used through its [public API](https://docs.sleeper.com/).

## Development

League Lens is a Go backend (Connect RPC, PostgreSQL, sqlc, Goose) with a React
and TypeScript frontend (Vite, TanStack Router and Query, Tailwind CSS,
shadcn/ui). In the Docker image, Go serves the built frontend.

To run from source you need Go 1.26+, Node 22.12+ with pnpm, Docker, and
[just](https://just.systems/):

```sh
just setup       # create .env.development
just tools       # install Air for live reload
just setup-web   # install frontend dependencies
just db-up       # start Postgres
just migrate     # apply migrations
just dev         # API and frontend with live reload
```

Open **http://127.0.0.1:5173**. Run `just` to list every command. See the
[development guide](docs/development.md) for testing, code generation, imports,
and the API. Design docs are in [docs/designs](docs/designs).

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.
It covers Conventional Commit messages, the local Git hooks, and the checks to
run before opening a pull request.

## License

League Lens is released under the [MIT License](LICENSE). ESPN, Sleeper, and
other data sources are subject to their own terms of service.

## Acknowledgments

- [DynastyProcess](https://github.com/dynastyprocess/data) for player ID
  mappings.
- [cwendt94/espn-api](https://github.com/cwendt94/espn-api) for documenting
  ESPN's fantasy API.
- [RotoWire](https://www.rotowire.com/) for player news, delivered through
  ESPN.
