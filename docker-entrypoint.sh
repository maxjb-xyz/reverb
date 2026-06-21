#!/bin/sh
# Reverb container entrypoint.
#
# Runs the app as the host user/group that owns your bind-mounted folders, so the
# SQLite DB is writable and spotDL downloads land on your filesystem with the
# right ownership (readable by your library server, your shell, etc.).
#
# Set PUID/PGID to the owner of your ./data and music directories — find them with
# `id -u` / `id -g`. Defaults to 1000:1000 (the typical first host user).
set -e

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"

# Reverb's own state dir is small and safe to take ownership of, so a freshly
# bind-mounted (often root-owned) ./data becomes writable with no host setup.
# We deliberately do NOT chown /music — it is usually a large, pre-existing
# library and you keep its ownership; it just needs to be writable by PUID:PGID
# (i.e. set PUID to its owner, or add that user to its group).
mkdir -p /data
chown -R "$PUID:$PGID" /data 2>/dev/null || true

# Give Python/spotDL a writable HOME for its cache/config (we may run as a uid
# with no /etc/passwd entry).
export HOME=/data

# Drop from root to the target uid:gid and exec Reverb with any passed flags.
exec gosu "$PUID:$PGID" reverb "$@"
