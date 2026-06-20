# Crate dev environment

1. Drop a few Creative-Commons audio files into `dev/music/` (e.g. tracks from
   https://freemusicarchive.org or the Navidrome demo set). They are gitignored
   except `.gitkeep`.
2. `docker compose -f docker-compose.dev.yml up` → Navidrome at http://localhost:4533
   (first run: create an admin user in the Navidrome UI).
3. Run Crate against it (M1 adds the Subsonic adapter):
   - `cd web && npm run dev`
   - `go run ./cmd/crate --dev`
   Open http://localhost:8090.

## Configure a Subsonic (Navidrome) library

Until the settings UI lands (M4a), point Crate at your Subsonic server by
seeding one `adapter_instances` row in the SQLite DB (default `./data/crate.db`),
then restart Crate. The password may be put in `config_json` or supplied via the
`CRATE_LIBRARY_PASSWORD` env var (env overrides `config_json`).

```sh
sqlite3 ./data/crate.db "INSERT INTO adapter_instances (id, type, name, enabled, priority, config_json) VALUES (
  'lib-subsonic', 'library', 'subsonic', 1, 0,
  json('{\"url\":\"http://localhost:4533\",\"username\":\"admin\",\"password\":\"YOUR_PASSWORD\"}')
);"
```

Or keep the secret out of the DB:

```sh
sqlite3 ./data/crate.db "INSERT INTO adapter_instances (id, type, name, enabled, priority, config_json) VALUES (
  'lib-subsonic', 'library', 'subsonic', 1, 0,
  json('{\"url\":\"http://localhost:4533\",\"username\":\"admin\"}')
);"
CRATE_LIBRARY_PASSWORD=YOUR_PASSWORD go run ./cmd/crate --dev
```

Then search your library, open album/artist pages, and play tracks (queue,
shuffle/repeat, drag-reorder all work). Streaming is proxied through
`/api/v1/stream/:id`, so your Subsonic credentials never reach the browser.
