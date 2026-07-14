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
mkdir music
docker compose up -d
```

Open http://localhost:8090 and complete the first-run wizard. Reverb uses the
`./music` folder, keeps its database in a managed Docker volume, and starts its
built-in music server and downloader automatically. Then add **Deezer** in
Settings for keyless catalog search.

Want to use an existing library, pin a release, or supply credentials? Download
the optional settings file and uncomment only the values you need:

```bash
curl -o .env https://raw.githubusercontent.com/maxjb-xyz/reverb/main/.env.example
```

- `REVERB_MUSIC_DIR=/srv/music` uses an existing music folder instead of `./music`.
- `REVERB_VERSION=0.1.0` pins the image instead of following `latest`.
- `REVERB_ADMIN_PASSWORD` skips the first-run password screen; remove it after
  the first boot.

To use your own library server, select **External Subsonic** in Settings →
Library backend and add its details there. Configure optional Spotify credentials
in `.env`.

## Folders

Reverb stores two things:

- **App state + SQLite DB** → the `reverb-data` **named volume** (`/data`). It needs
  no setup — the volume inherits the container's non-root ownership, so the DB just
  opens. (See [Backups](#backups) for copying it out.)
- **Your music library** → the `./music` **host folder** (`/music`), where spotDL
  downloads land and the bundled music server scans. Set `REVERB_MUSIC_DIR` in
  `.env` to use an existing library instead.

The container **runs non-root as uid 1000** (the typical first host user), so a
music folder you created/own is writable with **no `chown` and no `PUID`/`PGID`
config**. If your library is owned by a *different* user (e.g. a NAS share or a
service account), make it writable by uid 1000 before starting Reverb.

## The shared music folder

For downloads to appear in an external Subsonic/Navidrome server, it MUST scan
the same host music folder that you set with `REVERB_MUSIC_DIR`. After a download
completes, Reverb triggers a library scan and the track becomes playable.

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

> **Note:** Caddy sets `X-Forwarded-Proto` automatically. With nginx (below) you
> must set it yourself — the config already does.

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

## Security / exposing to the internet

Before you expose Reverb beyond a trusted LAN:

- **Put a TLS-terminating reverse proxy in front of it.** Reverb serves plain HTTP
  and relies on a same-origin session cookie; never expose port 8090 directly.
- **The proxy must set/overwrite `X-Forwarded-Proto`** so Reverb knows the original
  request was HTTPS. Caddy does this automatically; the nginx config above sets it
  explicitly (`proxy_set_header X-Forwarded-Proto $scheme;`).
- **Unset `REVERB_ADMIN_PASSWORD` after first boot.** It is read only on first run
  to seed the admin account and is ignored afterward — leaving it set just keeps a
  plaintext password in your environment.
- Keep the data volume sensitive — see [Secrets at rest](#secrets-at-rest).

## Backups

In the default `docker-compose.yml` setup the database does **not** live in a host
`./data` directory — it lives inside the **`reverb-data` named Docker volume**
(mounted at `/data`). Back up:

- The `reverb-data` named volume — holds the SQLite database (`/data/reverb.db`)
  plus app state. This is the only stateful Reverb data worth backing up.
- Your `./music` folder — holds the downloaded audio (managed by your library
  server).

**Named volume (default):** copy the DB out of the volume. A cold copy is simplest:

```bash
docker compose stop reverb
docker run --rm -v reverb_reverb-data:/data -v "$PWD/backups:/backup" \
  busybox cp /data/reverb.db /backup/reverb-$(date +%F).db
docker compose start reverb
```

(The volume is `<project>_reverb-data` — `reverb_reverb-data` when the compose
directory is `reverb`; run `docker volume ls` to confirm.) You can also
`docker cp reverb:/data/reverb.db ./reverb-$(date +%F).db` against a running
container, though a stopped-container copy is safest for SQLite.

**Bind-mount alternative:** if you changed `docker-compose.yml` to bind-mount the
data dir instead (e.g. `- ./data:/data`), then the DB really is at host
`./data/reverb.db` and you can back it up with a plain file copy while the
container is stopped.

### Secrets at rest

Adapter credentials (Spotify Client Secret, Subsonic/Navidrome password, Lidarr
API key) and the bundled-Navidrome admin password are currently stored
**unencrypted** in the SQLite database. Treat the data volume (and any DB backups
you copy out) as **sensitive**: restrict file permissions, keep backups off shared
storage, and don't commit them anywhere.

## Upgrades

```bash
docker compose pull       # fetch the new published image (bump REVERB_VERSION to pin)
docker compose up -d      # recreate the container
```

Building from source instead? Use `git pull && docker compose build && docker
compose up -d` (with the compose `build:` block uncommented).

Reverb runs SQLite migrations automatically on startup. Back up the database
before a major upgrade (see [Backups](#backups) — by default it lives in the
`reverb-data` named volume, not a host `./data` dir).

## spotDL version pin

The image pins `spotdl==4.5.0` (via the `SPOTDL_VERSION` build arg in the
Dockerfile). spotDL's stdout formatting is fragile and
Reverb parses download progress with the regex `(\d{1,3})\s*%`
(`internal/download/spotdl/adapter.go`). **Bumping the spotDL pin requires
re-validating that regex against the new output format** before shipping —
otherwise progress may silently degrade to "indeterminate".
