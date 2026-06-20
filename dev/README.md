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

## Configure a Spotify search source (pre-M4a)

M2 added Everywhere search via Spotify. Until the settings UI lands (M4), seed
a `search`-type `adapter_instances` row and supply the secret via env.

**Prerequisites:** a free [Spotify developer app](https://developer.spotify.com/dashboard)
— copy the `client_id` and `client_secret` from the app's dashboard.

### Step 1 — seed the adapter row

With Crate stopped:

```sh
sqlite3 data/crate.db "INSERT INTO adapter_instances (id,type,name,enabled,priority,config_json) \
  VALUES ('srch1','search','spotify',1,0,'{\"client_id\":\"YOUR_CLIENT_ID\"}');"
```

`client_secret` must **not** go into `config_json`; it is supplied via env only.

### Step 2 — start Crate

```sh
CRATE_ADMIN_PASSWORD=devpw CRATE_SPOTIFY_CLIENT_SECRET=YOUR_CLIENT_SECRET go run ./cmd/crate &
sleep 2
```

Expected log: `search sources active: 1` (plus `library adapter active: subsonic`
if the M1 library row is also seeded).

### Step 3 — smoke the SSE endpoint

Log in and get a session cookie:

```sh
curl -s -c /tmp/crate.cookies -X POST localhost:8090/api/v1/auth/login \
  -H 'Content-Type: application/json' -d '{"password":"devpw"}'
# expected: {"ok":true}
```

Stream a search (results arrive per-source as SSE events):

```sh
curl -sN -b /tmp/crate.cookies \
  "localhost:8090/api/v1/search/everywhere?q=daft%20punk&type=track" | head -c 600
```

Expected: one or more lines of the form
`data: {"source":"spotify","status":"ok","results":[...]}`.
Each result carries a `match` field: `in_library` if the track is also in your
Navidrome library, otherwise `not_in_library` (or absent when no library is
configured — the UI renders plain rows with no ✓).

In the frontend, toggle **Everywhere** in the search bar — results stream in and
appear in stable Tracks / Albums / Artists sections with a `spotify` source chip.
In-library rows show ✓ and clicking them plays the matched library track.

### Step 4 — confirm the secret never leaks

```sh
curl -s -b /tmp/crate.cookies "localhost:8090/api/v1/adapters/available" \
  | grep -i secret || echo "no secret value exposed"

curl -sN -b /tmp/crate.cookies \
  "localhost:8090/api/v1/search/everywhere?q=x&type=track" \
  | grep -i "YOUR_CLIENT_SECRET" || echo "secret not in SSE stream"
```

Expected: both commands print the `echo` fallback — the secret never reaches the browser.

### Tear down

```sh
kill %1 2>/dev/null
```
