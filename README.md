# Crate

**Crate** is a self-hosted music app that unifies your existing music library, the
broader catalog you can search online, and one-click downloading — in a single
fast web UI. It is a Go single-binary modular monolith with an embedded
React/TypeScript SPA.

> Crate is for personal use with music you have the legal right to download. See
> [Legal & ethical use](#legal--ethical-use).

## The core loop

1. **Search everywhere** — one search box spans your library and online sources
   (e.g. Spotify) at once, streaming results as each source responds.
2. **See what you already have** — results are matched against your library
   (by ISRC/metadata), so you instantly know what is missing.
3. **One-click download** — missing tracks download via spotDL into your music
   folder; live progress streams over a WebSocket.
4. **It just appears** — when the download finishes, Crate rescans your library
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

```bash
git clone https://github.com/maxjb-xyz/crate.git
cd crate
cp .env.example .env        # fill in secrets (see Configuration)
docker compose up -d        # builds + runs on http://localhost:8090
```

Open http://localhost:8090 and complete the **first-run wizard**: set an admin
password (unless you set `CRATE_ADMIN_PASSWORD` in `.env`), then add your
library, search, and downloader adapters in Settings. Point the spotDL
downloader's `output_dir` at `/music` so downloads land where your library
scans. Full details: [docs/deployment.md](docs/deployment.md).

## Configuration reference

Crate is configured by flags, environment variables, and the in-app Settings UI.
**Precedence: flags > environment > defaults.**

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--port` | `8090` | HTTP listen port |
| `--db` | `./data/crate.db` | SQLite database path |
| `--dev` | `false` | Dev mode (proxies the Vite dev server) |
| `--log-level` | `info` | Log level |

### Environment variables

| Variable | Description |
| --- | --- |
| `CRATE_PORT` | HTTP listen port (same as `--port`) |
| `CRATE_DB` | SQLite path (same as `--db`); the Docker image defaults this to `/data/crate.db` |
| `CRATE_DEV=1` | Enable dev mode |
| `CRATE_ADMIN_PASSWORD` | Seed the admin password on first run (if setup not yet complete) |
| `CRATE_AUTH_DISABLED=1` (or `true`) | Disable auth entirely — **trusted LAN only**, all routes become unauthenticated |
| `CRATE_SPOTIFY_CLIENT_SECRET` | Spotify search adapter Client Secret (overrides stored config) |
| `CRATE_LIBRARY_PASSWORD` | Subsonic/Navidrome library adapter password (overrides stored config) |

Secrets (`CRATE_*_SECRET`, `CRATE_*_PASSWORD`, `CRATE_ADMIN_PASSWORD`) should be
provided via environment / `.env` only — never committed. `.env` is gitignored;
`.env.example` is the committed template.

### First-run wizard

On first launch Crate detects that no admin password is set and shows a setup
screen. Set a password (or pre-seed `CRATE_ADMIN_PASSWORD`), then configure
adapters in Settings. Adapter config changes that require a restart surface a
"Restart to apply" banner.

## Legal & ethical use

Crate is a tool for **personal use with content you have the legal right to
access and download**. By using Crate you agree that:

- You are responsible for complying with the laws of your jurisdiction and the
  **terms of service** of every service you connect Crate to (your music server,
  Spotify, etc.). Crate does not grant any rights to content.
- **spotDL is a separate, third-party tool** that Crate invokes. Crate does not
  host, distribute, or provide any copyrighted content; it orchestrates tools you
  configure. How you use spotDL is your responsibility.
- Crate is intended for downloading music **you own or are otherwise legally
  entitled to** (e.g. content you have purchased or that is freely licensed). Do
  not use Crate to infringe copyright.
- Crate is provided **"as is", without warranty of any kind**. The authors are
  not liable for misuse. See the [LICENSE](LICENSE).

## Architecture overview

Crate is a **modular monolith**: a single Go binary organized around clean
**adapter seams** — `library` (Subsonic/Navidrome), `search` (Spotify), and
`downloader` (spotDL) — each registered explicitly at the composition root (no
`init()` side-effects). The frontend is a React/TypeScript SPA embedded into the
binary at build time (`-tags prod`). State and events flow through an in-process
EventBus that backs both the WebSocket and the download manager. The full design
rationale is in
[docs/superpowers/specs/2026-06-20-crate-mvp-design.md](docs/superpowers/specs/2026-06-20-crate-mvp-design.md).
The HTTP API is documented in OpenAPI, served live at `/api/v1/openapi.yaml`.

## Development & contributing

```bash
# Backend tests (scoped — never use ./... ; web/node_modules has vendored Go)
go test ./cmd/... ./internal/...

# Frontend (from web/)
cd web && npm install && npm run test

# Run locally (two shells)
cd web && npm run dev          # shell 1: Vite dev server
go run ./cmd/crate --dev       # shell 2: Go server proxying Vite

# End-to-end (hermetic, mocked)
cd web && npm run e2e
```

Contributions are welcome. Please open an issue to discuss substantial changes
first, keep tests green (`make test`), and follow the existing adapter/seam
patterns. New adapters should register at the composition root and ship with
tests.

## License

**AGPL-3.0-only** — chosen because Crate is a network-served, self-hosted app
that bundles GPL-family tooling (spotDL); AGPL keeps modifications open for a
networked service and matches the self-hosted-media-server tradition. See
[LICENSE](LICENSE).
