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
FROM golang:1.23 AS gobuild
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
# VERSION PIN: spotDL output formatting is fragile. Reverb's spotdl adapter parses
# progress with the regex `(\d{1,3})\s*%` in internal/download/spotdl/adapter.go.
# Bumping this pin REQUIRES re-validating that regex against the new output.
RUN pip install --no-cache-dir "spotdl==4.2.11"
COPY --from=gobuild /out/reverb /usr/local/bin/reverb
ENV REVERB_DB=/data/reverb.db
# spotDL is bundled and used as the default downloader out of the box; it writes
# into /music (the bind-mounted host library). Reverb auto-configures it when no
# downloader is set, so no Settings step is needed.
ENV REVERB_DOWNLOAD_DIR=/music
EXPOSE 8090
# Runs as root for a zero-config setup: it can read/write the bind-mounted host
# folders (./data, your music library) regardless of their ownership. Downloaded
# files are therefore owned by root on the host.
ENTRYPOINT ["reverb"]
