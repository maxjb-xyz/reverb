# Deploying Crate

Crate ships as a single Docker image: a static Go binary with the web UI
embedded, plus Python 3, ffmpeg, and a pinned spotDL. This guide covers a
production-ish single-host deployment.

## Quick start

```bash
cp .env.example .env      # fill in secrets
docker compose up -d      # builds + starts Crate on :8090
```

Open http://localhost:8090 and complete the first-run wizard (set an admin
password unless you provided `CRATE_ADMIN_PASSWORD` in `.env`), then add your
adapters in Settings:

- **Library** (Subsonic/Navidrome): point it at your existing server.
- **Search** (Spotify): set the Client ID in Settings; the Client Secret comes
  from `CRATE_SPOTIFY_CLIENT_SECRET` in `.env`.
- **Downloader** (spotDL): set `output_dir` to `/music`.

## The shared music volume

Crate's spotDL downloader writes into `/music`. For downloads to appear in your
library, your Subsonic/Navidrome server MUST scan the SAME directory. The
provided `docker-compose.yml` mounts `./music:/music` into Crate and (in the
commented Navidrome service) `./music:/music:ro` into Navidrome. After a
download completes, Crate triggers a library scan and the track becomes
playable.

## Reverse proxy + TLS

Run Crate behind a TLS-terminating reverse proxy. Crate serves plain HTTP on
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

## Volumes & backups

- `./data` → `/data` holds the SQLite database (`/data/crate.db`, set via
  `CRATE_DB`) plus app state. This is the only stateful Crate volume.
- `./music` → `/music` holds downloaded audio (shared with the library server).

**Backup:** stop the container (or use SQLite's online backup) and copy
`./data/crate.db`. A simple cold backup:

```bash
docker compose stop crate
cp ./data/crate.db ./backups/crate-$(date +%F).db
docker compose start crate
```

## Upgrades

```bash
git pull                  # or: docker compose pull (if using a published image)
docker compose build      # rebuild from the new source
docker compose up -d      # recreate the container
```

Crate runs SQLite migrations automatically on startup. Back up `./data/crate.db`
before a major upgrade.

## spotDL version pin

The image pins `spotdl==4.2.11`. spotDL's stdout formatting is fragile and
Crate parses download progress with the regex `(\d{1,3})\s*%`
(`internal/download/spotdl/adapter.go`). **Bumping the spotDL pin requires
re-validating that regex against the new output format** before shipping —
otherwise progress may silently degrade to "indeterminate".
