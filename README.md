# Reverb

**Reverb** is a self-hosted music app that unifies your existing music library, the
broader catalog you can search online, and one-click downloading — in a single
fast web UI. It is a Go single-binary modular monolith with an embedded
React/TypeScript SPA.

> Reverb is for personal use with music you have the legal right to download. See
> [Legal & ethical use](#legal--ethical-use).

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

## Screenshots

<!-- TODO: add screenshots of Search Everywhere, a download in progress, and the player. -->
_Screenshots coming soon._

## Quick start (Docker Compose)

No clone or build needed — Compose pulls the published image:

```bash
mkdir reverb && cd reverb
curl -O https://raw.githubusercontent.com/maxjb-xyz/reverb/main/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/maxjb-xyz/reverb/main/.env.example
# edit .env to set your secrets (see Configuration)
docker compose up -d        # pulls ghcr.io/maxjb-xyz/reverb and runs on http://localhost:8090
```

That's it — no permission setup. The DB lives in `./data` and music in `./music`
on your **host filesystem** (point `./music` at an existing library by editing the
volume line in `docker-compose.yml`). **spotDL is bundled and pre-configured as the
downloader**, so downloading works immediately. Pin a version with
`REVERB_VERSION=0.1.0` in `.env` (defaults to `latest`). Prefer to build from
source? See [Development & contributing](#development--contributing).

Open http://localhost:8090 and complete the **first-run wizard**: set an admin
password (unless you set `REVERB_ADMIN_PASSWORD` in `.env`), then connect your
**library** (Subsonic/Navidrome) and **search** (Spotify) adapters in Settings.
The **downloader (spotDL) is already configured** — nothing to set up. Full
details: [docs/deployment.md](docs/deployment.md).

## Configuration reference

Reverb is configured by flags, environment variables, and the in-app Settings UI.
**Precedence: flags > environment > defaults.**

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--port` | `8090` | HTTP listen port |
| `--db` | `./data/reverb.db` | SQLite database path |
| `--dev` | `false` | Dev mode (proxies the Vite dev server) |
| `--log-level` | `info` | Log level |

### Environment variables

| Variable | Description |
| --- | --- |
| `REVERB_PORT` | HTTP listen port (same as `--port`) |
| `REVERB_DB` | SQLite path (same as `--db`); the Docker image defaults this to `/data/reverb.db` |
| `REVERB_DEV=1` | Enable dev mode |
| `REVERB_ADMIN_PASSWORD` | Seed the admin password on first run (if setup not yet complete) |
| `REVERB_AUTH_DISABLED=1` (or `true`) | Disable auth entirely — **trusted LAN only**, all routes become unauthenticated |
| `REVERB_SPOTIFY_CLIENT_SECRET` | Spotify search adapter Client Secret (overrides stored config) |
| `REVERB_LIBRARY_PASSWORD` | Subsonic/Navidrome library adapter password (overrides stored config) |

Secrets (`REVERB_*_SECRET`, `REVERB_*_PASSWORD`, `REVERB_ADMIN_PASSWORD`) should be
provided via environment / `.env` only — never committed. `.env` is gitignored;
`.env.example` is the committed template.

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
