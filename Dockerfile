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
      -o /out/crate ./cmd/crate

# ---------- Stage 3: runtime ----------
FROM python:3.12-slim AS runtime
# ffmpeg is a spotDL runtime dependency.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*
# VERSION PIN: spotDL output formatting is fragile. Crate's spotdl adapter parses
# progress with the regex `(\d{1,3})\s*%` in internal/download/spotdl/adapter.go.
# Bumping this pin REQUIRES re-validating that regex against the new output.
RUN pip install --no-cache-dir "spotdl==4.2.11"
# Non-root user.
RUN useradd --create-home --uid 10001 crate
COPY --from=gobuild /out/crate /usr/local/bin/crate
ENV CRATE_DB=/data/crate.db
VOLUME ["/data"]
EXPOSE 8090
USER crate
ENTRYPOINT ["crate"]
