# SP3-3b: Last.fm Scrobbling — Design

**Status:** Approved (brainstorm 2026-06-30)
**Predecessor:** SP3-3a (listening history & stats) — shipped. The internal `playTracker` already detects qualifying plays (the Last.fm threshold) and the server records them via `POST /plays` → `play.Service.Record`. Scrobbling layers onto that pipeline.

## Goal

Let each Reverb user link their own Last.fm account and have their qualifying plays scrobbled to Last.fm in real time, with a live "Now Playing" indicator — robust against Last.fm outages and never blocking playback.

## Decisions (from brainstorm)

1. **Target: Last.fm only.** Built behind a `Scrobbler` interface in a registry so ListenBrainz/others can be added later, but only the Last.fm adapter ships now.
2. **Now Playing + scrobble.** Send `track.updateNowPlaying` on track start (fire-and-forget) AND scrobble qualifying plays (durable).
3. **New plays only.** Scrobbling begins at link time; no backfill of prior internal history (also sidesteps Last.fm's ~2-week timestamp limit).
4. **Admin registers the app credential.** The Last.fm api key + secret are a per-deployment admin setting (not shipped in the build). Each user then links their own account.
5. **Per-user linking lives in a new "Integrations" tab in account settings.**

## Architecture

Server-side scrobbling with a durable queue, hooked into the existing play pipeline.

Rejected alternatives: **client-side scrobbling** (would expose per-user session keys to the browser; CORS; no durability) and an **in-memory queue** (loses scrobbles across restarts / Last.fm outages).

```
track start ──FE playTracker──▶ POST /scrobble/nowplaying ──▶ scrobble.Service.NowPlaying ──▶ Last.fm updateNowPlaying (fire-and-forget, per linked user)
qualifying play ──FE playTracker──▶ POST /plays (existing) ──▶ play.Service.Record
                                                              └─(if user linked)─▶ enqueue scrobble_queue row
scrobble worker (background) ──drains queue──▶ Last.fm track.scrobble ──▶ success: done · failure: retry w/ backoff
```

## Components

### `internal/scrobble/` (new package)
- **`Scrobbler` interface** (the registry plugin):
  - `NowPlaying(ctx, creds, track) error`
  - `Scrobble(ctx, creds, plays []ScrobblePlay) (accepted int, err error)` — batch (Last.fm accepts up to 50/call)
  - `AuthURL(ctx) (url, token string, err error)` — begin linking
  - `CompleteAuth(ctx, token string) (sessionKey, username string, err error)` — finish linking
  - `creds` = the app key/secret (from adapter config) + the user's `session_key`.
- **`lastfm` adapter** — implements the interface against the Last.fm 2.0 API: MD5-signed calls (`api_sig = md5(sorted params + secret)`), JSON responses, the methods `auth.getToken`, `auth.getSession`, `track.updateNowPlaying`, `track.scrobble`. Lives behind the interface so it's swappable/testable against a fake HTTP server.
- **Scrobbler registry** — a new registry kind (the generic `internal/registry` machinery). The Last.fm adapter exposes a `ConfigSchema` with `api_key` and `api_secret` (`Secret:true`) so the admin configures it through the **existing adapter-config UI + secret redaction** (`redactConfig`/`mergeSecrets`; the browser never receives the secret).
- **`scrobble.Service`** — orchestrates: holds the active adapter + config; `NowPlaying(ctx, userID, track)` (look up the user's link → adapter.NowPlaying, ignore errors); `Enqueue(ctx, userID, play)` (insert a queue row iff the user has a healthy link); the **worker** loop draining `scrobble_queue`.

### Data model — migration `0021_scrobbling.sql` (additive)
- **`scrobble_link`** — per-user account link.
  `(user_id TEXT, provider TEXT, session_key TEXT, username TEXT, status TEXT /* active|broken */, created_at INTEGER, PRIMARY KEY(user_id, provider))`.
  `session_key` is stored server-side and **never returned by any API** (link endpoints expose `username` + `status` only).
- **`scrobble_queue`** — durable outbound scrobbles.
  `(id TEXT PK, user_id TEXT, provider TEXT, catalog_id TEXT, title TEXT, artist TEXT, album TEXT, duration_ms INTEGER, played_at INTEGER, status TEXT /* pending|done|failed */, attempts INTEGER, next_attempt_at INTEGER, created_at INTEGER)` + index `(status, next_attempt_at)`.
- App key/secret: stored in the scrobbler adapter's config (the existing adapter `config_json` + settings mechanism), not a new table.

## Flows

### Linking (per user, Last.fm desktop-auth)
1. Account → **Integrations** tab → "Connect Last.fm" → `POST /scrobble/lastfm/auth-url` → server `auth.getToken` → returns `{ authUrl, token }`. (400 if the admin hasn't configured the app key/secret yet — the tab shows "ask your admin to set up Last.fm".)
2. The user opens `authUrl` (`last.fm/api/auth?api_key=…&token=…`), approves, returns to Reverb.
3. "Finish connecting" → `POST /scrobble/lastfm/complete {token}` → server `auth.getSession(token)` → stores `session_key` + `username`, `status=active`. UI shows "Connected as `<username>`" + "Disconnect."
4. **Disconnect** → `DELETE /scrobble/lastfm` → removes the link row.

### Now Playing
On track start the `playTracker` POSTs `/scrobble/nowplaying {title,artist,album,durationMs}` → `scrobble.Service.NowPlaying(userID, …)` → if the user has an active link, `adapter.NowPlaying`. Fire-and-forget: errors logged, never surfaced, never queued.

### Scrobble
When a qualifying play is recorded (the existing `POST /plays` path), the server calls `scrobble.Service.Enqueue(userID, play)` — inserts a `scrobble_queue` row **iff** the user has an `active` link. The worker (ticker) selects due `pending` rows, batches per user (≤50), calls `adapter.Scrobble`; on success → `done`; on transient failure → `attempts++`, `next_attempt_at = now + backoff(attempts)`; after a cap → `failed`. Enqueue never blocks the `/plays` response.

### Broken link
If Last.fm returns an auth error (key 9 — invalid session), set `scrobble_link.status='broken'`, stop enqueuing for that user, and surface a "reconnect Last.fm" prompt in the Integrations tab.

## API (all under `requireAuth`; per-user via `cu.ID`)
- `POST /scrobble/lastfm/auth-url` → `{authUrl, token}`
- `POST /scrobble/lastfm/complete` `{token}` → `{username}`
- `DELETE /scrobble/lastfm`
- `GET /scrobble/links` → `[{provider, username, status}]` (never the session_key)
- `POST /scrobble/nowplaying` `{title,artist,album,durationMs}` → 204
- Admin app key/secret: via the existing adapter-config endpoints (scrobbler kind).

## Frontend
- **New "Integrations" tab in `Account.tsx`** — lists scrobble providers; "Connect Last.fm" / "Connected as `<user>`" + Disconnect / "reconnect" on broken / "admin hasn't configured Last.fm" when the app key is absent. Two-step connect (open authUrl in a new tab → "Finish connecting"). Tokens-only styling.
- **`playTracker`** — on track change, POST `/scrobble/nowplaying` for the new track (cheap, fire-and-forget; no effect on the existing qualifying-play logic).
- Admin integrations page gains the scrobbler adapter (app key/secret) via the existing adapters UI.

## Error handling & security
- **Durability:** the queue + retry/backoff survives Last.fm outages and restarts; playback and `/plays` are never blocked by scrobbling.
- **Secrets:** app secret uses the `Secret:true` config redaction (never sent to the browser); per-user `session_key` lives only in `scrobble_link` and is returned by no API.
- **Isolation:** every link/queue query is scoped to `cu.ID`; one user can never scrobble as another (DB-level test, like stats).
- **Now Playing** failures are swallowed (ephemeral); only scrobbles are durable.

## Testing
- **lastfm adapter:** `api_sig` MD5 correctness (known params → known signature); request shaping + response parsing for getToken/getSession/updateNowPlaying/scrobble, against a fake Last.fm HTTP server (incl. the auth-error path → broken-link).
- **Service/worker:** enqueue-iff-linked; worker drains pending → done; transient failure → backoff + retry; cap → failed; batching ≤50; per-user isolation.
- **Auth flow:** auth-url → complete → link stored; disconnect removes it.
- **Wiring:** a qualifying `/plays` enqueues a scrobble only when the user is linked; now-playing fire-and-forget ignores errors.
- **FE:** Integrations tab states (unconfigured / connect / connected / broken); playTracker posts now-playing on track change.

## Out of scope (documented)
- Backfilling prior internal history to Last.fm.
- ListenBrainz / other providers (the registry leaves room; not built now).
- Now-Playing retry/durability (intentionally ephemeral).
- Importing Last.fm history *into* Reverb stats.
