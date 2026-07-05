# syntax=docker/dockerfile:1

# ---------- Stage 1: build the web SPA ----------
FROM node:22-slim AS web
WORKDIR /app/web
# Install deps first (cache layer) — copy lockfile + manifest only.
COPY web/package.json web/package-lock.json ./
RUN npm ci
# Then the source, and build.
COPY web/ ./
RUN npm run build

# ---------- Stage 2: build the Go binary with the SPA embedded ----------
FROM golang:1.26 AS gobuild
ARG VERSION=dev
WORKDIR /src
# Module cache layer.
COPY go.mod go.sum ./
RUN go mod download
# Source.
COPY . .
# The prod embed requires internal/api/dist to be populated BEFORE the build.
COPY --from=web /app/web/dist ./internal/api/dist
# Static, cgo-free, prod-embedded, version-stamped.
RUN CGO_ENABLED=0 go build -tags prod \
      -ldflags "-X main.version=${VERSION}" \
      -o /out/reverb ./cmd/reverb

# ---------- Stage 3: runtime ----------
FROM python:3.12-slim AS runtime
# ffmpeg is a spotDL runtime dependency.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*
# Deno: current YouTube requires a JS runtime to solve its nsig/signature
# challenge. Without it, yt-dlp (and therefore spotDL) fails every download with
# "AudioProviderError: YT-DLP download error" and writes no file — spotDL itself
# prints "Some YouTube downloads require Deno". The official deno:bin image exists
# solely to be COPY'd in; /usr/local/bin is on PATH so yt-dlp finds it.
COPY --from=denoland/deno:bin /deno /usr/local/bin/deno
# Keep spotDL ITSELF current — that's the real fix for downloads "stuck at 0% /
# hangs on Processing query". That stage is ytmusicapi (the YouTube Music API),
# whose fix shipped in ytmusicapi 1.11.1 — gated behind spotdl >= 4.4.3. So the old
# 4.2.11 pin literally couldn't get the fix; bumping spotDL pulls a compatible
# ytmusicapi + yt-dlp. Pinned to a known-good latest via build arg for
# reproducibility — bump SPOTDL_VERSION + rebuild when downloads break again.
# yt-dlp (the actual download step) is additionally floated, since it goes stale
# between spotDL releases. Progress parsing degrades gracefully, so a spotDL
# output-format drift just falls back to an indeterminate spinner (never breaks).
ARG SPOTDL_VERSION=4.5.0
# spotDL is pinned for reproducibility, but yt-dlp is DELIBERATELY NOT pinnable:
# YouTube changes its signature/nsig scheme constantly, so a frozen yt-dlp goes
# stale within weeks and every download then fails with a bare "YT-DLP download
# error". Always install the latest at build time. NOTE: this only refreshes
# yt-dlp when the IMAGE is built — a long-running container still ages, so rebuild
# periodically (or `docker compose exec -u root reverb pip install --upgrade yt-dlp`
# to unstick a running one).
RUN pip install --no-cache-dir "spotdl==${SPOTDL_VERSION}" \
 && pip install --no-cache-dir --upgrade yt-dlp
COPY --from=gobuild /out/reverb /usr/local/bin/reverb
# --- Bundled Navidrome (GPL-3.0, shipped unmodified as a separate process) ---
# TARGETARCH is set automatically by buildx per target platform. The =amd64
# default only applies to a plain (non-buildx) 'docker build', so the from-source
# path works out of the box on x86; arm builders pass --build-arg TARGETARCH=arm64.
ARG TARGETARCH=amd64
ARG NAVIDROME_VERSION=0.62.0
RUN apt-get update \
 && apt-get install -y --no-install-recommends tini wget ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && wget -O /tmp/navidrome.tar.gz \
      "https://github.com/navidrome/navidrome/releases/download/v${NAVIDROME_VERSION}/navidrome_${NAVIDROME_VERSION}_linux_${TARGETARCH}.tar.gz" \
 && mkdir -p /tmp/nd \
 && tar -xzf /tmp/navidrome.tar.gz -C /tmp/nd \
 && install -m 0755 /tmp/nd/navidrome /usr/local/bin/navidrome \
 && rm -rf /tmp/navidrome.tar.gz /tmp/nd
# Non-root user (uid 1000 — the typical first host user, so a bind-mounted music
# library you own is writable with no setup). Create + own /data and /music BEFORE
# the VOLUME declaration so the `reverb-data` named volume inherits this ownership
# and the DB opens with zero host-side config.
RUN useradd --create-home --uid 1000 reverb \
 && mkdir -p /data /data/navidrome /music \
 && chown -R reverb:reverb /data /music
ENV REVERB_DB=/data/reverb.db
# spotDL is bundled and used as the default downloader out of the box; it writes
# into /music (the bind-mounted host library). Reverb auto-configures it when no
# downloader is set, so no Settings step is needed.
ENV REVERB_DOWNLOAD_DIR=/music
VOLUME ["/data"]
EXPOSE 8090
# Liveness probe: the public health endpoint responds as soon as the HTTP server
# is listening, so `restart: unless-stopped` can recover a wedged (but not
# crashed) process. Honors REVERB_PORT if overridden.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${REVERB_PORT:-8090}/api/v1/health" >/dev/null 2>&1 || exit 1
USER reverb
ENTRYPOINT ["/usr/bin/tini", "--", "reverb"]
