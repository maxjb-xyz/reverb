<p align="center">
  <img src="web/public/logo.png" alt="Reverb" width="120" />
</p>

<h1 align="center">Reverb</h1>
<p align="center">
  Self-hosted music, done right.<br/>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00798A?logo=go&logoColor=white" />
  <img src="https://img.shields.io/badge/React-19-149ECA?logo=react&logoColor=white" />
  <img src="https://img.shields.io/badge/Docker-ready-0B5EA8?logo=docker&logoColor=white" />
  <img src="https://img.shields.io/badge/license-AGPL_v3-A00000" />
</p>

**Reverb** is a self-hosted music app that unifies your existing music library, the
broader catalog you can search online, and one-click downloading — in a single
fast web UI. It is a Go single-binary modular monolith with an embedded
React/TypeScript SPA.

> Reverb is for personal use with music you have the legal right to download. See
> [Legal & ethical use](#legal--ethical-use).

## Screenshots

![Search Everywhere](docs/screenshots/search-everywhere.png)
_Search Everywhere — one box spans your library and online sources, with live
per-source streaming and library matching._

![A download in progress](docs/screenshots/download-in-progress.png)
_A one-click spotDL download in progress, with live WebSocket progress._

![The player](docs/screenshots/player.png)
_The web player — queue, shuffle, repeat, seek, and keyboard shortcuts._

## The core loop

1. **Search everywhere** — one search box spans your library and online sources
   (e.g. Spotify) at once, streaming results as each source responds.
2. **See what you already have** — results are matched against your library
   (by ISRC/metadata), so you instantly know what is missing.
3. **One-click download** — missing tracks download via spotDL into your music
   folder; live progress streams over a WebSocket.
4. **It just appears** — when the download finishes, Reverb rescans your library
   and the track flips to in-library — ready to play, in the same row.

## Features

- Unified library browsing (artists / albums / playlists) backed by a
  Subsonic/Navidrome server.
- Gapless-feeling web player with queue, shuffle, repeat, seek, and keyboard
  shortcuts.
- "Search Everywhere" with live per-source streaming (SSE) and library matching.
- One-click spotDL downloads with live progress and auto play-when-ready.
- Pluggable adapters (library / search / downloader) configured in-app, with a
  first-run setup wizard.
- Single static binary, SPA embedded; ships as one Docker image (Python3 +
  ffmpeg + pinned spotDL included).
- Responsive UI (desktop + mobile).

## Quick start (Docker Compose)

No clone or build needed — Compose pulls the published image:

```bash
mkdir reverb && cd reverb
curl -O https://raw.githubusercontent.com/maxjb-xyz/reverb/main/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/maxjb-xyz/reverb/main/.env.example
# edit .env to set your secrets (see Configuration)
docker compose up -d        # pulls ghcr.io/maxjb-xyz/reverb and runs on http://localhost:8090
```

That's it. Reverb runs **non-root** (uid 1000); your music library is the `./music`
folder on the **host filesystem** (point it at an existing library by editing the
volume line in `docker-compose.yml`), and the database lives in a managed Docker
volume — no permission setup for the common case. **spotDL is bundled and
pre-configured as the downloader**, so downloading works immediately. Pin a version
with `REVERB_VERSION=0.1.0` in `.env` (defaults to `latest`). Prefer to build from
source? See [Development & contributing](#development--contributing).

Open http://localhost:8090 and complete the **first-run wizard**: set an admin
password (unless you set `REVERB_ADMIN_PASSWORD` in `.env`), then add a
**search** (Spotify) adapter in Settings. The **library and downloader are
already configured** — the bundled Navidrome serves music from `/music`
automatically, and spotDL is pre-configured for downloads. To use your own
Navidrome/Subsonic instead, see [Library backends](#library-backends). Full
details: [docs/deployment.md](docs/deployment.md).

## Library backends

By default Reverb runs a **bundled music server** (Navidrome) inside the same
container — just mount your music at `/music` and start it. Nothing else to set up.

Prefer your own server? In **Settings → Library backend**, switch to **External
Subsonic** and add your Navidrome/Subsonic server under **Admin**. In external
mode nothing extra runs inside the Reverb container.

## Configuration reference

Reverb is configured by flags, environment variables, and the in-app Settings UI.
**Precedence: flags > environment > defaults.**

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--port` | `8090` | HTTP listen port |
| `--db` | `./data/reverb.db` | SQLite database path |
| `--dev` | `false` | Dev mode (proxies the Vite dev server) |

### Environment variables

| Variable | Description |
| --- | --- |
| `REVERB_PORT` | HTTP listen port (same as `--port`); defaults to `8090` |
| `REVERB_DB` | SQLite path (same as `--db`); the Docker image defaults this to `/data/reverb.db` |
| `REVERB_DEV=1` | Enable dev mode |
| `REVERB_DOWNLOAD_DIR` | Directory spotDL downloads into **and** the folder the bundled Navidrome serves. The Docker image defaults this to `/music` |
| `REVERB_ADMIN_PASSWORD` | Seed the admin password on first run only (ignored once setup is complete). **Unset it after first boot.** |
| `REVERB_SPOTIFY_CLIENT_ID` | Spotify app Client ID (alternative to setting it in the Settings UI) |
| `REVERB_SPOTIFY_CLIENT_SECRET` | Spotify search adapter Client Secret (overrides stored config) |
| `REVERB_LIBRARY_PASSWORD` | Subsonic/Navidrome library adapter password (overrides stored config) |
| `REVERB_SPOTDL_PATH` | Path to the spotDL binary. Defaults to the bundled one; rarely needed |
| `REVERB_NAVIDROME_BIN` | Path to the Navidrome binary for bundled library mode. Defaults to the bundled one; rarely needed |

Secrets (`REVERB_*_SECRET`, `REVERB_*_PASSWORD`, `REVERB_ADMIN_PASSWORD`) should be
provided via environment / `.env` only — never committed. `.env` is gitignored;
`.env.example` is the committed template. `REVERB_ADMIN_PASSWORD` is read **only on
first run** to seed the admin account; once setup is complete it is ignored, so
unset it after the first boot rather than leaving a plaintext password in your
environment.

### Exposing Reverb to the internet

Reverb serves plain HTTP and relies on a same-origin session cookie. Before you
expose it beyond a trusted LAN, put it behind a **TLS-terminating reverse proxy**
(Caddy, nginx, Traefik, …). The proxy MUST set/overwrite the `X-Forwarded-Proto`
header so Reverb can tell that the original request was HTTPS. See
[docs/deployment.md](docs/deployment.md#reverse-proxy--tls) for ready-to-use
Caddy and nginx configs.

### First-run wizard

On first launch Reverb detects that no admin password is set and shows a setup
screen. Set a password (or pre-seed `REVERB_ADMIN_PASSWORD`), then configure
adapters in Settings. Adapter config changes that require a restart surface a
"Restart to apply" banner.

## Legal & ethical use

Reverb is a tool for **personal use with content you have the legal right to
access and download**. By using Reverb you agree that:

- You are responsible for complying with the laws of your jurisdiction and the
  **terms of service** of every service you connect Reverb to (your music server,
  Spotify, etc.). Reverb does not grant any rights to content.
- **spotDL is a separate, third-party tool** that Reverb invokes. Reverb does not
  host, distribute, or provide any copyrighted content; it orchestrates tools you
  configure. How you use spotDL is your responsibility.
- Reverb is intended for downloading music **you own or are otherwise legally
  entitled to** (e.g. content you have purchased or that is freely licensed). Do
  not use Reverb to infringe copyright.
- Reverb is provided **"as is", without warranty of any kind**. The authors are
  not liable for misuse. See the [LICENSE](LICENSE).

## Architecture overview

Reverb is a **modular monolith**: a single Go binary organized around clean
**adapter seams** — `library` (Subsonic/Navidrome), `search` (Spotify), and
`downloader` (spotDL) — each registered explicitly at the composition root (no
`init()` side-effects). The frontend is a React/TypeScript SPA embedded into the
binary at build time (`-tags prod`). State and events flow through an in-process
EventBus that backs both the WebSocket and the download manager. The full design
rationale is in
[docs/superpowers/specs/2026-06-20-reverb-mvp-design.md](docs/superpowers/specs/2026-06-20-reverb-mvp-design.md).
The HTTP API is documented in OpenAPI, served live at `/api/v1/openapi.yaml`.

## Development & contributing

```bash
# Backend tests (scoped — never use ./... ; web/node_modules has vendored Go)
go test ./cmd/... ./internal/...

# Frontend (from web/)
cd web && npm install && npm run test

# Run locally (two shells)
cd web && npm run dev          # shell 1: Vite dev server
go run ./cmd/reverb --dev       # shell 2: Go server proxying Vite

# End-to-end (hermetic, mocked)
cd web && npm run e2e
```

Contributions are welcome. Please open an issue to discuss substantial changes
first, keep tests green (`make test`), and follow the existing adapter/seam
patterns. New adapters should register at the composition root and ship with
tests.

## License

**AGPL-3.0-only** — chosen because Reverb is a network-served, self-hosted app
that bundles GPL-family tooling (spotDL); AGPL keeps modifications open for a
networked service and matches the self-hosted-media-server tradition. See
[LICENSE](LICENSE).
