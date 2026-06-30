# Reverb — Product & Architecture Plan

> The self-hosted music app that knows what you have, and knows how to get what you don't.

---

## Vision

The self-hosted music ecosystem has excellent individual tools — Navidrome for serving, Lidarr for automating downloads, spotDL for grabbing from Spotify — but no single experience ties them together. Reverb is that experience.

The core loop Reverb enables:

1. Search for any song, album, or artist — from your library or anywhere on the internet
2. If you have it, play it. If you don't, download it — in one click
3. It appears in your library and starts playing

This is what streaming services do effortlessly. Reverb brings it to self-hosters, with no subscriptions, no DRM, and full control over your data.

---

## Design Philosophy

**Unified over Siloed.** The line between "what I have" and "what exists" should blur. The UI doesn't force you into a mode — it just tells you what's available and lets you act.

**Pluggable over Opinionated.** Reverb doesn't care if you use spotDL or Lidarr. It provides the experience layer; you configure the tools underneath.

**Self-hosted First.** Single Docker container. No cloud accounts required. Works entirely on your LAN if you want it to.

**Progressive Enhancement.** Useful with just a music folder. More powerful with each integration you add.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                          Reverb                              │
│                                                              │
│   ┌──────────────────────┐   ┌──────────────────────────┐   │
│   │     Web Frontend     │   │      Reverb Server        │   │
│   │   (React + TypeScript│   │   (Go — single binary)   │   │
│   │    Spotify-like UI)  │   │                          │   │
│   └──────────┬───────────┘   └──────────┬───────────────┘   │
│              └──────────────────────────┘                    │
│                            │                                 │
│          ┌─────────────────┼──────────────────┐             │
│          ▼                 ▼                  ▼             │
│   ┌─────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│   │   Library   │  │    Search    │  │ Download Manager │  │
│   │   Adapter   │  │   Sources    │  │    & Queue       │  │
│   └─────────────┘  └──────────────┘  └──────────────────┘  │
│          │                 │                  │             │
│   ┌──────┴──┐      ┌───────┴──────┐   ┌──────┴───────┐    │
│   │Navidrome│      │Spotify API   │   │   Lidarr     │    │
│   │(Subsonic│      │MusicBrainz   │   │   spotDL     │    │
│   │  API)   │      │Deezer / etc. │   │   (custom)   │    │
│   │  — or — │      └──────────────┘   └──────────────┘    │
│   │Local dir│                                              │
│   └─────────┘                                              │
└──────────────────────────────────────────────────────────────┘
```

### Key Principle: Adapter Pattern Throughout

Every integration — library source, search provider, downloader — implements a defined interface. Reverb ships with first-party adapters; the community can build more.

---

## Core Interfaces

### Library Adapter
Provides the local music collection. Reverb talks to one of:
- **Navidrome / any Subsonic-compatible server** (recommended — battle-tested, handles transcoding, metadata)
- **Direct folder scan** (for users who don't run Navidrome — Reverb reads files and serves them itself)

```typescript
interface LibraryAdapter {
  search(query: string): Promise<Track[]>
  getArtist(id: string): Promise<Artist>
  getAlbum(id: string): Promise<Album>
  stream(trackId: string): ReadableStream
  getPlaylists(): Promise<Playlist[]>
}
```

### Search Source
Queries external catalogs for music that may not be in the library yet.

```typescript
interface SearchSource {
  name: "spotify" | "musicbrainz" | "deezer" | string
  search(query: string, type: "track" | "album" | "artist"): Promise<SearchResult[]>
  getAlbum(externalId: string): Promise<ExternalAlbum>
}
```

### Downloader
Accepts a download request and handles acquiring the file.

```typescript
interface Downloader {
  name: "lidarr" | "spotdl" | string
  canDownload(track: ExternalTrack): Promise<boolean>
  enqueue(item: DownloadRequest): Promise<DownloadJob>
  getQueue(): Promise<DownloadJob[]>
  onComplete(handler: (job: DownloadJob) => void): void
}
```

---

## Feature Breakdown

### MVP — Phase 1

The minimum that makes Reverb worth using.

**Music Player**
- Spotify-like layout: sidebar (Library, Playlists, Artists, Albums), main content area, persistent player bar
- Gapless playback, queue management, shuffle/repeat
- Waveform scrubber, keyboard shortcuts
- Connect to existing Navidrome via Subsonic API

**Unified Search**
- Single search bar, two modes toggled inline:
  - `My Library` — searches your Navidrome/local collection
  - `Everywhere` — searches configured external sources (Spotify catalog by default)
- Results visually distinguished:
  - ✓ **In Library** — play immediately
  - ↓ **Available** — click to download
  - (no icon) — result from external catalog only

**One-Click Download**
- Clicking an unavailable track opens a small popover: "Download with [spotDL / Lidarr]" (or auto-selects if only one configured)
- Download queued, progress shown in a persistent tray in the sidebar
- Track automatically appears in library when complete (via polling or Navidrome webhook)

**Configuration UI**
- Web-based settings page (no manual YAML editing required, though YAML is supported)
- Add/remove: library source, search sources, downloaders
- Test connection buttons for each integration

---

### v1 — Phase 2

Polishing the experience and expanding integrations.

**Artist Pages — The Killer Feature**
- Full artist discography pulled from external source (MusicBrainz/Spotify)
- Albums marked: ✓ In Library | ↓ Download All | partial (X/Y tracks)
- Feels like Spotify's artist page, but with download affordances baked in

**Download Queue Panel**
- Dedicated view showing active, queued, and completed downloads
- Retry failed downloads
- Cancel in-progress downloads

**Playlist Sync**
- Import a Spotify playlist URL → Reverb shows which tracks you have and which you're missing
- "Download Missing" button — queues everything you don't have
- Scheduled sync: keep a playlist fresh as it updates

**Lidarr Integration**
- Full two-way sync: artists you follow in Reverb can be monitored in Lidarr
- Lidarr quality profiles selectable per download request

**Smart Library Indicators**
- "You have 7 of 12 albums by this artist" shown on artist cards
- Missing albums surfaced as gentle recommendations

---

### Future — Phase 3+

**Standalone Mode**
- Reverb's own music server (no Navidrome required)
- Built-in file watcher, metadata scraping, transcoding

**Mobile Apps**
- Native iOS and Android clients (React Native or Flutter)
- Offline download support

**Social / Multi-User**
- Request system: other users on your server can request songs without download access
- Listening history and stats (Last.fm scrobbling built in)

**Plugin Marketplace**
- Community-contributed search sources and downloaders
- e.g., Tidal, Qobuz, SoulSeek, Deemix adapters

---

## Tech Stack

| Layer | Choice | Rationale |
|---|---|---|
| **Backend** | Go | Single binary, easy self-hosting, excellent for streaming, strong concurrency for download queue |
| **Frontend** | React + TypeScript | Widest contributor pool, rich ecosystem for audio (Howler.js, wavesurfer.js) |
| **Styling** | Tailwind CSS | Fast iteration, consistent design system |
| **Database** | SQLite (via sqlc) | Zero-dependency, perfect for single-user self-hosted |
| **Streaming** | HTTP Range requests | Native browser support, seekable without re-encoding |
| **Real-time** | WebSockets | Download progress, library scan updates |
| **Config** | YAML + web UI | Human-editable file + friendly UI for non-technical users |
| **Packaging** | Docker (single container) | Standard for self-hosted; also ship a standalone binary |

---

## UI / UX Design Direction

Reverb should feel like a premium music app, not a homelab admin panel.

**Visual Identity**
- Dark UI (near-black `#0D0D0F` background) — music apps live in the dark
- Accent: a cool electric indigo-violet (`#7C6AF7`) — different from Spotify's green, signals "this is yours"
- Subtle glassmorphism on player bar and download tray
- Typography: `Inter` for UI, `Cal Sans` or `Bricolage Grotesque` for headings
- Album art as the dominant visual element — let covers breathe

**Search UX (the core interaction)**
```
┌─────────────────────────────────────────────────────┐
│  🔍  Search...                    [ My Library ▾ ]  │
└─────────────────────────────────────────────────────┘
                                         ┌────────────┐
                                         │ My Library │
                                         │ Everywhere │
                                         └────────────┘
```

Results in "Everywhere" mode:
```
  Tracks
  ────────────────────────────────────────────
  ✓  Karma Police          Radiohead · OK Computer     3:58
  ↓  Exit Music (For a Film) Radiohead · OK Computer   4:25
     Bones                  Radiohead · Pablo Honey     2:34
  ✓  Creep                  Radiohead · Pablo Honey     3:58
```

`✓` = in library (play on click)
`↓` = not in library (download popover on click)
no icon = external result, downloader uncertain

**Player Bar**
- Fixed at the bottom, always visible
- Album art thumbnail, track/artist, progress bar, controls, volume
- Download tray toggle on the right: shows active download badge count

---

## Deployment

**Docker Compose (recommended)**
```yaml
services:
  reverb:
    image: reverb/reverb:latest
    ports:
      - "8090:8090"
    volumes:
      - ./config:/config
      - ./music:/music        # only needed for standalone mode
    environment:
      - REVERB_CONFIG=/config/reverb.yaml
```

**Example config (reverb.yaml)**
```yaml
library:
  type: navidrome
  url: http://navidrome:4533
  username: admin
  password: secret

search_sources:
  - type: spotify
    client_id: your_client_id
    client_secret: your_client_secret

downloaders:
  - type: spotdl
    output_dir: /music
  - type: lidarr
    url: http://lidarr:8686
    api_key: your_api_key

ui:
  port: 8090
```

---

## Open Questions / Decisions to Make

1. **Go vs. Node.js for backend?** Go produces a single binary and handles streaming elegantly, but Node/TypeScript would lower the barrier for contributors and makes API integrations slightly easier. Go is the stronger self-hosted choice long-term.

2. **Build on Subsonic API or build our own music server?** Starting with Subsonic (Navidrome integration) gets us to MVP fastest. Standalone mode can come later. Should be an explicit design decision so the library adapter is properly abstracted from day one.

3. **How to handle "already downloading" state?** If two users request the same track simultaneously, the download queue needs deduplication logic.

4. **Downloader fallback chain?** If Lidarr can't find a track, should Reverb automatically fall back to spotDL? Should this be configurable?

5. **Legal / ethical framing?** The README should clearly position Reverb as a tool for managing music you have rights to. The downloader integrations are user-configured; Reverb doesn't ship with any credentials or accounts.

6. **Name collision check** — is "Reverb" already a significant open-source project in this space?

---

## Immediate Next Steps

1. Scaffold the Go backend with Subsonic API client
2. Scaffold the React frontend with a static Spotify-like layout (sidebar + player bar)
3. Wire up library search against a running Navidrome instance
4. Add spotDL as the first downloader — shell exec wrapper with output parsing
5. Build the unified search result component with local vs. external distinction
6. Implement the one-click download popover → queue → library refresh loop

---

## Future Stuff

- Reverb Wrapped: End of year review.
- Discover Weekly: Grabs songs you'd be interested in from your downloader. Needs an algorithm backend.
- SUB/WAVE: Either a plugin to extend functionality or just absorb the codebase itself. Or equivalent functionality.
- Desktop Apps (Windows, Mac, and Linux): Built with Tauri?
- Mobile Apps
- SSO/OIDC
- Custom themed player bars (tron lightcycle, lightsaber, etc.)
- Device control: control devices logged into your account
- Search feature searches spotify playlists
- Listening Parties with other users
- Federated features
- Discord integration

---

*Reverb — own your music, again.*
