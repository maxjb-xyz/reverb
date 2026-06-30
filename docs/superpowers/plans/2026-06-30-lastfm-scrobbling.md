# SP3-3b: Last.fm Scrobbling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax. Each implementer follows superpowers:test-driven-development — the plan gives exact interfaces, schemas, contracts, the signing algorithm, and the test cases; implementers TDD the routine bodies.

**Goal:** Let each Reverb user link their own Last.fm account and have qualifying plays scrobbled (with live "Now Playing"), via a server-side durable queue hooked into the existing `/plays` pipeline.

**Architecture:** A new `internal/scrobble` package: a `Scrobbler` interface (registry kind) with a `lastfm` adapter (MD5-signed Last.fm 2.0 API), a `scrobble.Service` (per-user link lookup + a durable `scrobble_queue` worker), wired so a qualifying `POST /plays` enqueues a scrobble iff the user is linked, and the FE posts now-playing on track change. Per-user linking lives in a new "Integrations" tab in account settings.

**Tech Stack:** Go 1.23, modernc sqlite + goose migrations + sqlc, chi; React 19 + TS, TanStack Query, Tailwind tokens, Vitest.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-06-30-lastfm-scrobbling-design.md`. Decisions: Last.fm only (registry-extensible); Now Playing + scrobble; **new plays only (no backfill)**; **admin registers the app key/secret**; per-user link in a **new account Integrations tab**.
- Per-user privacy: every link/queue query scoped to the session user (`cu.ID`); a user can NEVER scrobble as another. The `session_key` and app `api_secret` are stored server-side and returned by NO API (reuse the `Secret:true` config redaction in `internal/api/adapters_secrets.go`; link endpoints expose `username`+`status` only).
- Scrobbling NEVER blocks playback or the `/plays` response. Now-Playing is fire-and-forget (errors swallowed); scrobbles are durable (queue + backoff).
- Scrobble threshold = the existing internal qualifying-play (it already IS the Last.fm spec) — no new threshold.
- Next migration number is `0021`. sqlc is v1.31.1 (regenerate with it to keep the generated diff minimal). FE tokens-only (no raw hex / `text-black`/`text-white`; accent uses `text-on-accent`). Gate per task: `go test ./internal/... && go build ./... && go vet ./...` and/or `cd web && npx vitest run <files> && npx tsc --noEmit && npm run build`. Commit footer: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. Work on branch `feat/lastfm-scrobbling` (already created; spec committed @ 88a1a09). Never touch main.

---

## File Structure

- `internal/store/migrations/0021_scrobbling.sql` — `scrobble_link` + `scrobble_queue` (additive).
- `internal/store/queries/scrobble.sql` — link CRUD + queue enqueue/select-due/mark-*.
- `internal/scrobble/scrobble.go` — `Scrobbler` interface, `Creds`, `Track`, `ScrobblePlay`, the registry instance.
- `internal/scrobble/lastfm/adapter.go` — Last.fm adapter (MD5 signing + 4 API methods).
- `internal/scrobble/service.go` — `Service` (NowPlaying, Enqueue, worker loop) + narrow Querier iface.
- `internal/api/scrobble.go` — HTTP handlers (auth-url, complete, disconnect, links, nowplaying).
- `internal/api/server.go` / `cmd/reverb/main.go` — routes + construction + scrobbler registry registration.
- `web/src/lib/scrobbleApi.ts` — FE client.
- `web/src/routes/Account.tsx` + `web/src/components/account/IntegrationsTab.tsx` — the new Integrations tab.
- `web/src/lib/playTracker.ts` + `AppShell.tsx` — now-playing on track change.

---

## Task 1: Migration + queries (data layer)

**Files:** Create `internal/store/migrations/0021_scrobbling.sql`, `internal/store/queries/scrobble.sql`; regenerate `internal/store/db/*`; Test: `internal/store/scrobble_store_test.go`.

**Produces:** tables + sqlc methods: `UpsertScrobbleLink`, `GetScrobbleLink(userID,provider)`, `ListScrobbleLinks(userID)`, `DeleteScrobbleLink(userID,provider)`, `SetScrobbleLinkStatus(status,userID,provider)`; `InsertScrobbleQueue`, `SelectDueScrobbles(now,limit)`, `MarkScrobbleDone(id)`, `MarkScrobbleRetry(attempts,nextAttemptAt,id)`, `MarkScrobbleFailed(id)`.

- [ ] **Step 1 — migration** (additive; `Down` drops both):
```sql
-- +goose Up
CREATE TABLE scrobble_link (
  user_id     TEXT NOT NULL,
  provider    TEXT NOT NULL,
  session_key TEXT NOT NULL,
  username    TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'active', -- active | broken
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (user_id, provider)
);
CREATE TABLE scrobble_queue (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL,
  provider        TEXT NOT NULL,
  catalog_id      TEXT NOT NULL DEFAULT '',
  title           TEXT NOT NULL,
  artist          TEXT NOT NULL,
  album           TEXT NOT NULL DEFAULT '',
  duration_ms     INTEGER NOT NULL DEFAULT 0,
  played_at       INTEGER NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending', -- pending | done | failed
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL
);
CREATE INDEX idx_scrobble_queue_due ON scrobble_queue (status, next_attempt_at);
-- +goose Down
DROP TABLE scrobble_queue;
DROP TABLE scrobble_link;
```
- [ ] **Step 2 — queries** `scrobble.sql`: `SelectDueScrobbles` = `WHERE status='pending' AND next_attempt_at <= ? ORDER BY next_attempt_at LIMIT ?`. All link queries keyed by `user_id` (+ `provider`). Regenerate sqlc (`go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`); commit regenerated `db/*`.
- [ ] **Step 3 — RED→GREEN test** (real in-memory store, like `internal/store/*_test.go`): upsert a link → Get returns it; ListScrobbleLinks scoped per user (user A's link absent for user B); InsertScrobbleQueue + SelectDueScrobbles returns due pending rows only (a future `next_attempt_at` is excluded); MarkScrobbleDone/Retry/Failed transition status. **Gate:** `go test ./internal/store/ && go build ./...`. **Commit.**

---

## Task 2: Scrobbler interface + Last.fm adapter (the signed API)

**Files:** Create `internal/scrobble/scrobble.go`, `internal/scrobble/lastfm/adapter.go`; Test: `internal/scrobble/lastfm/adapter_test.go`.

**Interfaces — Produces:**
```go
// internal/scrobble/scrobble.go
package scrobble
type Track struct { Title, Artist, Album string; DurationMs int }
type ScrobblePlay struct { Track; PlayedAt int64 } // unix seconds
type Creds struct { APIKey, APISecret, SessionKey string }
type Scrobbler interface {
    NowPlaying(ctx context.Context, c Creds, t Track) error
    Scrobble(ctx context.Context, c Creds, plays []ScrobblePlay) (accepted int, err error)
    AuthURL(ctx context.Context, c Creds) (authURL, token string, err error)         // c needs APIKey/APISecret
    CompleteAuth(ctx context.Context, c Creds, token string) (sessionKey, username string, err error)
}
var ErrAuth = errors.New("scrobble: provider rejected credentials") // map Last.fm error code 9 (invalid session) → ErrAuth
```
The registry: reuse `internal/registry`. Register `"lastfm"` factory returning the adapter; the adapter exposes a `ConfigSchema` with fields `api_key` and `api_secret` (`Secret:true`) so the admin adapters UI + `redactConfig`/`mergeSecrets` handle it.

**Last.fm API facts (implement against these):** base `https://ws.audioscrobbler.com/2.0/`, all POST form-encoded, `&format=json`.
- **Signing:** `api_sig = md5( concat(for each param sorted by key: key+value) + api_secret )` — EXCLUDING `format` and `callback`. Then send all params + `api_sig` + `format=json`.
- `auth.getToken` (sign with api_key) → `{token}`. `AuthURL` returns `https://www.last.fm/api/auth/?api_key=<key>&token=<token>` + the token.
- `auth.getSession` (params: api_key, method, token; signed) → `{session:{key,name}}` → `(sessionKey, username)`.
- `track.updateNowPlaying` (params: artist, track, album?, duration?, api_key, method, sk; signed).
- `track.scrobble` batched: `artist[i], track[i], timestamp[i], album[i]?, duration[i]?, api_key, method, sk` (i = 0..49); signed; returns accepted/ignored counts.
- Any response `{"error":9,...}` → return `ErrAuth`; other errors → a normal error (transient).

- [ ] **Step 1 — RED: signature test.** Known vector: params `{api_key:"k", method:"auth.getToken"}`, secret `"s"` → assert `apiSig(params,"s") == md5("api_keykmethodauth.getTokens")` (compute the expected hex in the test). Implement `apiSig`.
- [ ] **Step 2 — RED: API calls against a fake server.** Use `httptest.Server`; point the adapter's base URL at it. Assert: `auth.getToken` parses `{token}`; `AuthURL` builds the right URL; `CompleteAuth` posts a signed `auth.getSession` and returns `(key,name)` from `{session:{key,name}}`; `NowPlaying`/`Scrobble` post the expected signed params (assert `api_sig` present + `sk` present + correct method); a `{"error":9}` body → `ErrAuth`. Implement the adapter (inject base URL + an `*http.Client` for testability).
- [ ] **Step 3 — GREEN + Gate:** `go test ./internal/scrobble/... && go build ./... && go vet ./...`. **Commit.**

---

## Task 3: Service + durable worker

**Files:** Create `internal/scrobble/service.go`; Test: `internal/scrobble/service_test.go`.

**Consumes:** Task 1 queries; Task 2 `Scrobbler`/`Creds`/`Track`/`ScrobblePlay`/`ErrAuth`.
**Produces:**
```go
type Querier interface { /* the Task-1 link + queue methods the service uses */ }
type Service struct { /* q Querier; sc Scrobbler; cfg func() Creds (api_key/secret from adapter config); now func() time.Time; idgen func() string */ }
func NewService(q Querier, sc Scrobbler, cfg func() Creds, now func() time.Time, idgen func() string) *Service
func (s *Service) NowPlaying(ctx context.Context, userID string, t Track)            // looks up active link; fire-and-forget (logs, never returns to caller path)
func (s *Service) Enqueue(ctx context.Context, userID string, p ScrobblePlay) error  // inserts a queue row IFF the user has an active link; else no-op nil
func (s *Service) RunWorker(ctx context.Context, tick time.Duration)                 // ticker loop: drain due rows
func (s *Service) drainOnce(ctx context.Context, batch int) error                    // exported-for-test or lowercase + test via RunWorker; see step
// Auth passthroughs used by the API layer:
func (s *Service) AuthURL(ctx) (url, token string, err error)
func (s *Service) CompleteAuth(ctx, userID, token string) (username string, err error) // stores the link
func (s *Service) Links(ctx, userID) ([]Link, error)   // {Provider,Username,Status} — NEVER SessionKey
func (s *Service) Unlink(ctx, userID, provider string) error
```
**Backoff:** `next = now + min(cap, base * 2^attempts)` (e.g. base 60s, cap 1h); after `maxAttempts` (e.g. 6) → `MarkScrobbleFailed`. On `ErrAuth` during a scrobble → `SetScrobbleLinkStatus('broken')` for that user and stop (don't burn attempts). Batch ≤50 per user per drain.

- [ ] **Step 1 — RED tests** (fake Scrobbler capturing calls; real store): (a) `Enqueue` for a linked user inserts a pending row; for an UNlinked user inserts nothing. (b) `drainOnce` with a due row calls `Scrobbler.Scrobble` and marks it `done`. (c) a transient `Scrobble` error → row stays pending, `attempts` incremented, `next_attempt_at` advanced. (d) `ErrAuth` → link set `broken` + enqueue now no-ops. (e) per-user isolation: user A's due row never carries user B's session key. (f) `CompleteAuth` stores a link via a fake Scrobbler returning `(key,name)`; `Links` returns `{username,status}` and NO session key field exists on the type.
- [ ] **Step 2 — implement** Service + worker. `NowPlaying` swallows errors. `cfg()` reads the api_key/secret from the scrobbler adapter config.
- [ ] **Step 3 — GREEN + Gate** `go test ./internal/scrobble/... && go build ./...`. **Commit.**

---

## Task 4: API handlers + routes + /plays wiring + construction

**Files:** Create `internal/api/scrobble.go`; Modify `internal/api/server.go` (routes + `Deps.Scrobble`), `internal/api/plays.go` (enqueue on qualifying play), `cmd/reverb/main.go` (construct service, register lastfm adapter, start worker); Test: `internal/api/scrobble_test.go`.

**Consumes:** Task 3 `Service`.
**Endpoints** (all under `requireAuth` / `pr` group; user from `currentUser(r)`, never the body):
- `POST /scrobble/lastfm/auth-url` → `{authUrl, token}` (400 if app key/secret unconfigured).
- `POST /scrobble/lastfm/complete` `{token}` → `{username}`.
- `DELETE /scrobble/lastfm` → 204.
- `GET /scrobble/links` → `[{provider, username, status}]` (lowercase json tags; NEVER `sessionKey`).
- `POST /scrobble/nowplaying` `{title, artist, album, durationMs}` (lowercase json tags) → 204; calls `Service.NowPlaying` (returns 204 even if unlinked / errors — fire-and-forget).
- `Deps.Scrobble *scrobble.Service`; nil → 503 on the above.

**/plays wiring:** in the existing qualifying-play path (`handlePlay` after `Record` succeeds, or inside `Record`), call `s.deps.Scrobble.Enqueue(ctx, cu.ID, scrobble.ScrobblePlay{Track:{title,artist,album,durationMs}, PlayedAt: playedAt})`. Enqueue must not fail the 204 (log on error). Decide placement in the handler (keeps `play.Service` free of the scrobble dep) — call `Enqueue` from `handlePlay` after a successful `Record`.

- [ ] **Step 1 — RED tests** (api harness): each endpoint session-scoped (a second user's link never appears in user-1's `GET /scrobble/links`); `complete` stores a link (fake Scrobbler); `auth-url` 400 when app key absent; `nowplaying` → 204; `GET /scrobble/links` response JSON has NO `sessionKey`/`session_key` key; a qualifying `POST /plays` for a linked user inserts a `scrobble_queue` row (DB-level assert) and for an unlinked user inserts none; unauth → 401.
- [ ] **Step 2 — implement** handlers + routes + the `/plays` enqueue + `main.go` construction (build `scrobble.NewService`, register the `lastfm` adapter in a new scrobbler registry, start `RunWorker` in a goroutine with `context.WithoutCancel`-style lifetime tied to the server). Use the LOCAL sqlc; `cfg()` pulls api_key/secret from the scrobbler adapter config (admin-set).
- [ ] **Step 3 — GREEN + Gate** `go test ./internal/api/ && go build ./... && go vet ./...`. **Commit.**

---

## Task 5: FE — scrobbleApi + Account "Integrations" tab + admin adapter config

**Files:** Create `web/src/lib/scrobbleApi.ts`, `web/src/components/account/IntegrationsTab.tsx`; Modify `web/src/routes/Account.tsx` (add the tab); Test: `IntegrationsTab.test.tsx`.

**Consumes:** Task 4 endpoints (match the lowercase json exactly).
**scrobbleApi:** `getLinks()`, `lastfmAuthUrl()` → `{authUrl, token}`, `lastfmComplete(token)` → `{username}`, `lastfmDisconnect()`, `nowPlaying(track)`.

- [ ] **Step 1 — RED tests** (mock scrobbleApi): the Integrations tab renders, by state: **unconfigured** (auth-url 400 / a `configured:false` signal) → "Ask your admin to set up Last.fm"; **not linked** → "Connect Last.fm" button → calls `lastfmAuthUrl`, opens `authUrl` in a new tab, shows a "Finish connecting" button → `lastfmComplete(token)` → "Connected as `<username>`"; **active** → username + "Disconnect" (→ `lastfmDisconnect`); **broken** → "Reconnect" prompt. Tokens-only (grep JSX).
- [ ] **Step 2 — implement** the tab + wire it into `Account.tsx` as a new "Integrations" tab (mirror the page's existing tab/section pattern). The admin app key/secret reuses the existing admin adapters config UI for the new scrobbler kind (verify the adapters page renders an arbitrary registry kind generically; if it special-cases kinds, add `scrobbler` to its list — keep that change minimal).
- [ ] **Step 3 — GREEN + Gate** `cd web && npx vitest run src/components/account/IntegrationsTab.test.tsx src/routes/Account.test.tsx && npx tsc --noEmit && npm run build`. **Commit.**

---

## Task 6: FE — Now Playing on track change

**Files:** Modify `web/src/lib/playTracker.ts` (or a sibling `nowPlaying.ts`), `web/src/components/AppShell.tsx`; Test: `playTracker.test.ts` (extend) or `nowPlaying.test.ts`.

**Consumes:** Task 5 `scrobbleApi.nowPlaying`.
- [ ] **Step 1 — RED test:** on a track-change event from the engine, `nowPlaying({title,artist,album,durationMs})` is called once with the NEW track; not called on pause/seek/progress; errors are swallowed (a rejecting `nowPlaying` doesn't throw). Mirror the existing `playTracker.test.ts` engine-fake harness.
- [ ] **Step 2 — implement:** subscribe to track changes (the playTracker already sees engine state) and fire `nowPlaying` for the new track. Keep it independent of the qualifying-play accrual logic (don't disturb Task-6/SP3-3a behavior). Wire in `AppShell` (the playTracker is already started there).
- [ ] **Step 3 — GREEN + Gate** `cd web && npx vitest run src/lib/playTracker.test.ts && npx tsc --noEmit && npm run build`. **Commit.**

---

## Task 7: Full gate + whole-branch review + merge

- [ ] **Step 1 — full gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build`. Add a hermetic e2e for the Integrations tab if cheap (mirror existing specs; default-mock `/scrobble/links` in `installApiMocks` so other specs are unaffected — like the notification/stats default mocks). gofmt the touched Go files.
- [ ] **Step 2 — whole-branch review** (most capable model): per-user privacy (session_key never leaves the server; all queries scoped); the MD5 signing correctness; durability/backoff; FE↔BE JSON casing for `/scrobble/nowplaying` + `/scrobble/links` (the masked-by-mock bug class); now-playing doesn't disturb the qualifying-play logic; tokens-only. Triage the ledger minors.
- [ ] **Step 3 — fix blockers, then ff-merge `feat/lastfm-scrobbling` → local main.** (Do NOT push; the user pushes when ready.)

---

## Deferred / out of scope (documented in spec): backfill; ListenBrainz; now-playing retry; importing Last.fm history into Reverb stats.
