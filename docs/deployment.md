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
password unless you provided `REVERB_ADMIN_PASSWORD` in `.env`), then connect:

- **Library** (Subsonic/Navidrome): point it at your existing server.
- **Search** (Spotify): set the Client ID in Settings; the Client Secret comes
  from `REVERB_SPOTIFY_CLIENT_SECRET` in `.env`.
- **Downloader**: nothing to do — **spotDL is bundled and pre-configured** to
  write into `/music`.

## Folders

Reverb stores two things:

- **App state + SQLite DB** → the `reverb-data` **named volume** (`/data`). It needs
  no setup — the volume inherits the container's non-root ownership, so the DB just
  opens. (See [Backups](#backups) for copying it out.)
- **Your music library** → the `./music` **host bind mount** (`/music`), where spotDL
  downloads land and your library server scans. To use an existing library, change
  `./music` to its path in `docker-compose.yml` (e.g. `- /srv/music:/music`).

The container **runs non-root as uid 1000** (the typical first host user), so a
music folder you created/own is writable with **no `chown` and no `PUID`/`PGID`
config**. If your library is owned by a *different* user (e.g. a NAS share or a
service account), either `chown` it to `1000:1000` or add a `user:` line to the
`reverb` service matching its owner:

```yaml
    user: "1001:1001"   # set to `id -u`:`id -g` of your music folder's owner
```

## The shared music folder

For downloads to appear in your library, your Subsonic/Navidrome server MUST scan
the SAME `/music` folder. The provided `docker-compose.yml` bind-mounts `./music`
into Reverb and (in the commented Navidrome service) the same folder read-only
into Navidrome. After a download completes, Reverb triggers a library scan and the
track becomes playable.

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

- The `reverb-data` volume holds the SQLite database (`/data/reverb.db`) plus app
  state — the only stateful Reverb data worth backing up.
- Your `./music` folder holds the downloaded audio (managed by your library server).

Copy the DB out of the named volume (cold copy is simplest):

```bash
docker compose stop reverb
docker run --rm -v reverb_reverb-data:/data -v "$PWD/backups:/backup" \
  busybox cp /data/reverb.db /backup/reverb-$(date +%F).db
docker compose start reverb
```

(The volume is `<project>_reverb-data` — `reverb_reverb-data` when the compose
directory is `reverb`; run `docker volume ls` to confirm.)

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
