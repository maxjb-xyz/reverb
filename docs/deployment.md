# Deploying Reverb

Reverb ships as a single Docker image: a static Go binary with the web UI
embedded, plus Python 3, ffmpeg, and a pinned spotDL. This guide covers a
production-ish single-host deployment.

## Quick start

Compose pulls the published image (`ghcr.io/maxjb-xyz/reverb`) — no source
checkout or build required:

```bash
mkdir reverb && cd reverb
curl -O https://raw.githubusercontent.com/maxjb-xyz/reverb/main/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/maxjb-xyz/reverb/main/.env.example
# edit .env to set secrets; optionally pin REVERB_VERSION=0.1.0 (defaults to latest)
docker compose up -d      # pulls the image and starts Reverb on :8090
```

Open http://localhost:8090 and complete the first-run wizard (set an admin
password unless you provided `REVERB_ADMIN_PASSWORD` in `.env`), then add your
adapters in Settings:

- **Library** (Subsonic/Navidrome): point it at your existing server.
- **Search** (Spotify): set the Client ID in Settings; the Client Secret comes
  from `REVERB_SPOTIFY_CLIENT_SECRET` in `.env`.
- **Downloader** (spotDL): set `output_dir` to `/music`.

## Folders & permissions (PUID/PGID)

Reverb bind-mounts two host folders:

- `./data` → `/data` — app state + the SQLite DB (`/data/reverb.db`).
- `MUSIC_DIR` → `/music` — your music library (where downloads land and your
  library server scans). Point `MUSIC_DIR` at an existing library (e.g.
  `/srv/music` or a NAS mount) in `.env`; it defaults to `./music`.

The container starts as root only to fix `/data` ownership, then **drops to the
`PUID:PGID` user** (via `gosu`) and runs Reverb non-root. Set `PUID`/`PGID` in
`.env` to the owner of your folders so the DB is writable and downloads land with
the right ownership:

```bash
id -u   # -> PUID
id -g   # -> PGID
```

`/data` is chowned to `PUID:PGID` automatically on start, so it works even if
Docker created `./data` as root. `/music` is **not** chowned (it may be a large
existing library you don't want re-owned) — it must already be writable by
`PUID:PGID`. If `PUID` doesn't match your music dir's owner, downloads fail with
permission errors; fix by setting `PUID` to that owner or adding it to the dir's
group. (If `/data` is unwritable you'll see `unable to open database file ... (14)`,
SQLITE_CANTOPEN, in a restart loop — almost always a `PUID` mismatch.)

## The shared music folder

Reverb's spotDL downloader writes into `/music`. For downloads to appear in your
library, your Subsonic/Navidrome server MUST scan the SAME folder. The provided
`docker-compose.yml` bind-mounts `MUSIC_DIR` into Reverb and (in the commented
Navidrome service) the same folder read-only into Navidrome. After a download
completes, Reverb triggers a library scan and the track becomes playable. Run
Navidrome with the same `PUID:PGID` (or at least read access) so it can read the
files Reverb writes.

## Reverse proxy + TLS

Run Reverb behind a TLS-terminating reverse proxy. Reverb serves plain HTTP on
8090 and uses a same-origin session cookie + a WebSocket at `/api/v1/ws`, so the
proxy MUST forward Upgrade/Connection headers.

### Caddy (simplest)

```
music.example.com {
    reverse_proxy localhost:8090
}
```

Caddy obtains/renews certificates automatically and proxies WebSockets out of
the box.

### nginx

```nginx
server {
    listen 443 ssl;
    server_name music.example.com;
    ssl_certificate     /etc/letsencrypt/live/music.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/music.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8090;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## Backups

- `./data` holds the SQLite database (`/data/reverb.db`) plus app state — the only
  stateful Reverb data worth backing up.
- Your `MUSIC_DIR` holds the downloaded audio (managed by your library server).

Since `./data` is a host folder, a cold backup is just a copy:

```bash
docker compose stop reverb
cp ./data/reverb.db ./backups/reverb-$(date +%F).db
docker compose start reverb
```

## Upgrades

```bash
docker compose pull       # fetch the new published image (bump REVERB_VERSION to pin)
docker compose up -d      # recreate the container
```

Building from source instead? Use `git pull && docker compose build && docker
compose up -d` (with the compose `build:` block uncommented).

Reverb runs SQLite migrations automatically on startup. Back up `./data/reverb.db`
before a major upgrade.

## spotDL version pin

The image pins `spotdl==4.2.11`. spotDL's stdout formatting is fragile and
Reverb parses download progress with the regex `(\d{1,3})\s*%`
(`internal/download/spotdl/adapter.go`). **Bumping the spotDL pin requires
re-validating that regex against the new output format** before shipping —
otherwise progress may silently degrade to "indeterminate".
