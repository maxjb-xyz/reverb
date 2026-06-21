# Reverb M4a — Config UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task is a self-contained unit: a fresh implementer with ZERO prior context can complete it from the file paths, interfaces, and complete code given here. Tasks are ordered store queries → sqlc regen → registry-shared adapter helpers → secret-redaction core → adapters REST API (list/create/update/delete/test) → settings REST API → pending-restart flag → composition wiring → frontend api clients + hooks → AdapterForm + TestConnection → Settings page → first-run wizard → accent-color live application → smoke.

**Goal:** Make Reverb installable and configurable entirely through the browser — removing the M1/M2/M3 requirement to hand-seed `adapter_instances` rows with SQL. A brand-new user runs `docker compose up`, opens the UI, sets an admin password, then adds a Library (Subsonic/Navidrome), a Search source (Spotify), and a Downloader (spotDL) through `ConfigSchema`-driven forms — each with a **Test Connection** button — and uses Reverb. The settings page and the first-run wizard share the SAME `AdapterForm` + TestConnection components. Secrets are never leaked to the browser (Secret-typed fields are redacted to an `isSet` boolean; blank-on-update preserves the stored value). The accent color is configurable (default red `#F0354B`) and applied live to the `--color-accent` CSS variable.

**Architecture — apply-config decision: RESTART-TO-APPLY (option A).** The composition root (`cmd/reverb/main.go` + `*_wiring.go`) builds the active library/search/downloader adapters and the Manager/Aggregator EXACTLY ONCE at startup from `adapter_instances`. M4a does NOT add a hot-reload seam: there is no clean low-risk place to atomically swap a running `download.Manager` (worker goroutines, in-flight `dl.Start` execs, the scan-debounce timer) and the SSE `Aggregator` (in-flight per-source goroutines, open SSE streams) without race risk that would blow the M4a budget. Instead, adapter create/update/delete and settings writes persist to the DB and flip an in-memory **config-dirty-since-startup** flag. The API exposes that flag (`GET /api/v1/config/pending-restart` and a `pendingRestart` boolean echoed on each mutation response); the Settings UI shows an honest **"Restart Reverb to apply changes"** banner. Reads (`GET /adapters`, `GET /adapters/available`) work without restart because they query the DB/registry directly, not the live adapters. accent_color/dynamic_background settings DO take effect live (they are pure frontend display settings — no banner needed for those two). Hot-reload is explicitly deferred to M4b/later. This keeps M4a simple, robust, and honest.

The flag is a single `*atomic.Bool` constructed in `main.go`, satisfied by a tiny `ConfigDirty` interface in `api.Deps`; the adapter/settings mutation handlers set it; `GET /config/pending-restart` reads it. Nil-safe: when the Deps field is nil the API reports `pendingRestart:false`.

**Tech Stack:** Go 1.23, chi v5, `net/http`, `net/http/httptest`, `sync/atomic`, `github.com/google/uuid` (already a dependency, for instance IDs), sqlc v1.31.1 (installed; engine sqlite) for the new `adapter_instances` queries. SQLite modernc only. React 19, TypeScript ~6, Vite 8, Vitest 4, Tailwind 3.4 (accent token `--color-accent` already wired in `web/src/index.css` + `web/tailwind.config.js`), React Router 6, TanStack Query 5, Zustand 4 (all already in `web/`). Frontend tests stub `fetch`; no real network.

## Global Constraints

- Go module `github.com/maximusjb/reverb`; Go 1.23; SQLite modernc only; sqlc engine sqlite (regen via the installed `sqlc` binary, fallback `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`).
- **Secrets:** NEVER return stored secret values to the browser. Redact `Secret:true` fields (per each adapter's `ConfigSchema`) → an `isSet` boolean / masked placeholder. On update, a blank/omitted secret field PRESERVES the existing stored value (never wipes it). Secrets remain overridable by env at `Init` (unchanged — the composition root applies env just before `Init`).
- **Apply-config = restart-to-apply (option A).** Persist to DB + flip an in-memory config-dirty flag; the UI shows a "Restart Reverb to apply changes" banner; the change loads on next start. No live adapter rebuild in M4a.
- **Self-describing:** forms + TestConnection are driven by each adapter's `ConfigSchema` — no hardcoded per-adapter UI. The wizard and the Settings page share the `AdapterForm` + TestConnection components.
- **accent_color** (default red `#F0354B`) + **dynamic_background** settings via `/settings`; accent applied live to the `--color-accent` CSS var (hex → `"r g b"` channels). These two settings apply live (no restart banner).
- All adapter/settings endpoints behind `requireAuth`. Deps additions are nil-safe; every existing `api.NewServer(api.Deps{...})` call site still compiles.
- **Tests:** Go via `httptest` with a fake/real registry + a temp `store.Store` (adapter CRUD, secret redaction/preserve, `/adapters/test` via a fake adapter whose `TestConnection` is controllable). Frontend Vitest stubbing `fetch` (AdapterForm renders from a schema, Test button, secret masking; Settings list/add/remove/toggle; wizard flow). Run Go with `go test ./cmd/... ./internal/...` (NOT `./...`); frontend with `cd web && npm run test` (Vitest) and typecheck via `cd web && npm run build`.
- TDD always: failing test → confirm RED → minimal code → confirm GREEN → conventional-commit. sqlc-generated code is committed.

---

## File Structure

**Go (backend) — created/modified in M4a:**

| Path | Responsibility |
|---|---|
| `internal/store/queries/adapters.sql` | MODIFY: add `GetAdapterInstance`, `UpdateAdapterInstance`, `SetAdapterInstanceEnabled`, `SetAdapterInstancePriority` queries (Create/List/Delete reused). |
| `internal/store/db/adapters.sql.go` | REGENERATED by sqlc (committed): new params + methods. |
| `internal/store/store_test.go` | MODIFY: add an adapter_instance CRUD round-trip (get-by-id, update, set-enabled, set-priority). |
| `internal/api/adapters_secrets.go` | NEW: generic secret redaction/merge using a `ConfigSchema` (no per-adapter hardcoding). |
| `internal/api/adapters_secrets_test.go` | NEW: redaction (omit secret values, emit `isSet`) + merge (blank preserves, present overwrites). |
| `internal/api/adapters.go` | NEW: REST handlers — list configured, create, update (PUT), delete, test. Modify `handleAdaptersAvailable` to also include the Library registry. |
| `internal/api/adapters_test.go` | NEW: handler tests (list redacted, create, update secret-preserve, delete, test ok/error, auth, pending-restart flips). |
| `internal/api/settings.go` | NEW: `GET /settings` + `PUT /settings` (accent_color, dynamic_background; non-secret). |
| `internal/api/settings_test.go` | NEW: get defaults, put + get round-trip, auth. |
| `internal/api/config.go` | NEW: `GET /config/pending-restart` handler. |
| `internal/api/config_test.go` | NEW: pending-restart reflects the flag; nil-safe. |
| `internal/api/server.go` | MODIFY: add `Adapters`, `Settings`, `ConfigDirty` to `Deps`; mount the new routes; reuse `Search`/`Downloader`/`Library` registries for `/adapters/available` + `/adapters/test`. |
| `cmd/reverb/main.go` | MODIFY: construct an `*atomic.Bool` dirty flag; pass `st.Q()` directly as `api.Deps.Adapters` — `*db.Queries` satisfies `AdapterStore` without any wrapper. |
| `cmd/reverb/config_dirty.go` | NEW: `atomicDirty` wrapper satisfying `api.ConfigDirty`. |

**React (frontend) — created/modified in M4a, under `web/`:**

| Path | Responsibility |
|---|---|
| `src/lib/api.ts` | MODIFY: add `put` and `del` methods to the typed fetch wrapper. |
| `src/lib/adaptersApi.ts` | NEW: typed REST client + TanStack hooks for available/configured adapters CRUD + test. |
| `src/lib/adaptersApi.test.tsx` | NEW: Vitest stubbing fetch (list/create/update/delete/test shapes). |
| `src/lib/settingsApi.ts` | NEW: typed settings client + hooks + `hexToRgbChannels` + `applyAccent`. |
| `src/lib/settingsApi.test.ts` | NEW: hex→"r g b" conversion + applyAccent sets the CSS var. |
| `src/components/AdapterForm.tsx` | NEW: renders a form from a `ConfigSchema` (text/password[secret]/number/bool) + a Test Connection button. |
| `src/components/AdapterForm.test.tsx` | NEW: renders fields from a schema, secret masking, Test button calls the API. |
| `src/routes/Settings.tsx` | REWRITE: sections per adapter type; list/add/edit/remove/enable-toggle/reorder; accent picker + dynamic_background toggle; pending-restart banner. |
| `src/routes/Settings.test.tsx` | NEW: list renders, add opens form, remove/toggle call the API, banner shows when dirty. |
| `src/routes/Setup.tsx` | REWRITE → multi-step wizard: ① admin password (existing) → ② Library → ③ Search → ④ Downloader → finish. Reuses AdapterForm. |
| `src/routes/Setup.test.tsx` | MODIFY: step-1 still prompts for password; advancing to step 2 shows the Library step. |
| `src/main.tsx` | MODIFY: bootstrap the accent color from `/settings` before first paint (best-effort). |

---

## Task 1: Store — adapter_instances get-by-id + update + set-enabled/priority queries + sqlc regen

**Files:**
- Modify: `internal/store/queries/adapters.sql`
- Regenerate (committed): `internal/store/db/adapters.sql.go`
- Test: `internal/store/store_test.go` (append a CRUD round-trip)

**Interfaces:**
- Consumes: sqlc (engine sqlite), the existing `adapter_instances` table (columns `id, type, name, enabled, priority, config_json, created_at, updated_at` — NO new migration needed).
- Produces (sqlc-generated on `*db.Queries`):
  ```go
  func (q *Queries) GetAdapterInstance(ctx, id string) (db.AdapterInstance, error)
  func (q *Queries) UpdateAdapterInstance(ctx, arg db.UpdateAdapterInstanceParams) error          // {Name, Enabled, Priority, ConfigJson, ID}
  func (q *Queries) SetAdapterInstanceEnabled(ctx, arg db.SetAdapterInstanceEnabledParams) error  // {Enabled, ID}
  func (q *Queries) SetAdapterInstancePriority(ctx, arg db.SetAdapterInstancePriorityParams) error// {Priority, ID}
  ```
  - Existing reused: `CreateAdapterInstance`, `ListAdapterInstances`, `DeleteAdapterInstance`.

- [ ] **Step 1: Add the queries**

Edit `internal/store/queries/adapters.sql` — append below the existing three queries:
```sql
-- name: GetAdapterInstance :one
SELECT * FROM adapter_instances WHERE id = ?;

-- name: UpdateAdapterInstance :exec
UPDATE adapter_instances
SET name = @name,
    enabled = @enabled,
    priority = @priority,
    config_json = @config_json,
    updated_at = unixepoch()
WHERE id = @id;

-- name: SetAdapterInstanceEnabled :exec
UPDATE adapter_instances SET enabled = @enabled, updated_at = unixepoch() WHERE id = @id;

-- name: SetAdapterInstancePriority :exec
UPDATE adapter_instances SET priority = @priority, updated_at = unixepoch() WHERE id = @id;
```

> **sqlc note:** named params (`@name`, `@id`, ...) keep the generated param struct field names stable and readable. `UpdateAdapterInstance` emits `UpdateAdapterInstanceParams{Name string; Enabled int64; Priority int64; ConfigJson string; ID string}`. `SetAdapterInstanceEnabledParams{Enabled int64; ID string}`. `SetAdapterInstancePriorityParams{Priority int64; ID string}`. Do NOT mix positional `?` with named `@` in the same statement.

- [ ] **Step 2: Regenerate sqlc (installed binary; fallback go run)**

Run from the repo root:
```bash
sqlc generate || go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
```
Expected: no output on success. `internal/store/db/adapters.sql.go` gains the four new methods + their param structs.

Verify:
```bash
grep -n "func (q \*Queries) GetAdapterInstance" internal/store/db/adapters.sql.go
grep -n "type UpdateAdapterInstanceParams struct" internal/store/db/adapters.sql.go
grep -n "type SetAdapterInstanceEnabledParams struct" internal/store/db/adapters.sql.go
grep -n "type SetAdapterInstancePriorityParams struct" internal/store/db/adapters.sql.go
```
Expected: all four print a match.

- [ ] **Step 3: Write the failing CRUD round-trip test**

Append to `internal/store/store_test.go` (the file already imports `context`, `database/sql` is NOT needed here, and `github.com/maximusjb/reverb/internal/store/db`; add `"github.com/google/uuid"` to the import block if missing):
```go
func TestAdapterInstanceCRUD(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/ai.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := st.Q()

	id := uuid.NewString()
	if err := q.CreateAdapterInstance(ctx, db.CreateAdapterInstanceParams{
		ID: id, Type: "search", Name: "spotify", Enabled: 1, Priority: 0,
		ConfigJson: `{"client_id":"abc","client_secret":"shh"}`,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := q.GetAdapterInstance(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "spotify" || got.Enabled != 1 {
		t.Fatalf("get mismatch: %+v", got)
	}

	if err := q.UpdateAdapterInstance(ctx, db.UpdateAdapterInstanceParams{
		Name: "spotify", Enabled: 1, Priority: 5, ConfigJson: `{"client_id":"new"}`, ID: id,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = q.GetAdapterInstance(ctx, id)
	if got.Priority != 5 || got.ConfigJson != `{"client_id":"new"}` {
		t.Fatalf("update did not persist: %+v", got)
	}

	if err := q.SetAdapterInstanceEnabled(ctx, db.SetAdapterInstanceEnabledParams{Enabled: 0, ID: id}); err != nil {
		t.Fatalf("set-enabled: %v", err)
	}
	got, _ = q.GetAdapterInstance(ctx, id)
	if got.Enabled != 0 {
		t.Fatalf("enabled not toggled: %+v", got)
	}

	if err := q.SetAdapterInstancePriority(ctx, db.SetAdapterInstancePriorityParams{Priority: 9, ID: id}); err != nil {
		t.Fatalf("set-priority: %v", err)
	}
	got, _ = q.GetAdapterInstance(ctx, id)
	if got.Priority != 9 {
		t.Fatalf("priority not set: %+v", got)
	}

	if err := q.DeleteAdapterInstance(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := q.GetAdapterInstance(ctx, id); err == nil {
		t.Fatal("expected error getting a deleted instance")
	}
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/store/ -run AdapterInstanceCRUD -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/queries/adapters.sql internal/store/db/adapters.sql.go internal/store/store_test.go
git commit -m "feat(store): adapter_instance get-by-id, update, set-enabled/priority queries"
```

---

## Task 2: Generic secret redaction + merge (`internal/api/adapters_secrets.go`)

**Files:**
- Create: `internal/api/adapters_secrets.go`
- Test: `internal/api/adapters_secrets_test.go`

**Interfaces:**
- Consumes: `internal/registry` (`registry.ConfigSchema`, `registry.ConfigField`).
- Produces:
  ```go
  // secretSentinel is the placeholder the API returns for a SET secret. The browser
  // never receives the real value; submitting this sentinel back (or a blank string)
  // means "keep the stored secret".
  const secretSentinel = "••••••••"

  // redactConfig returns a copy of cfg with every Secret:true field (per schema)
  // replaced: the value is removed and a parallel "<key>__isSet" boolean is added.
  // Non-secret fields pass through unchanged.
  func redactConfig(schema registry.ConfigSchema, cfg map[string]any) map[string]any

  // mergeSecrets returns the config to PERSIST: starting from incoming, for every
  // Secret:true field whose incoming value is blank or the sentinel, it restores the
  // value from stored (preserve-on-blank). Non-secret fields take the incoming value.
  func mergeSecrets(schema registry.ConfigSchema, stored, incoming map[string]any) map[string]any

  // overlayEnvSecret is unchanged from the wiring files (env wins at Init); not here.
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/api/adapters_secrets_test.go`:
```go
package api

import (
	"testing"

	"github.com/maximusjb/reverb/internal/registry"
)

func schema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "client_id", Label: "Client ID", Type: "string", Required: true},
		{Key: "client_secret", Label: "Client Secret", Type: "string", Required: true, Secret: true},
	}}
}

func TestRedactConfigHidesSecretValueEmitsIsSet(t *testing.T) {
	out := redactConfig(schema(), map[string]any{"client_id": "abc", "client_secret": "shh"})
	if out["client_id"] != "abc" {
		t.Fatalf("non-secret should pass through, got %v", out["client_id"])
	}
	if _, present := out["client_secret"]; present {
		t.Fatalf("secret VALUE must not be returned, got %v", out["client_secret"])
	}
	if out["client_secret__isSet"] != true {
		t.Fatalf("expected client_secret__isSet=true, got %v", out["client_secret__isSet"])
	}
}

func TestRedactConfigUnsetSecretIsSetFalse(t *testing.T) {
	out := redactConfig(schema(), map[string]any{"client_id": "abc"})
	if out["client_secret__isSet"] != false {
		t.Fatalf("expected isSet=false for absent secret, got %v", out["client_secret__isSet"])
	}
	if _, present := out["client_secret"]; present {
		t.Fatal("absent secret must not appear")
	}
}

func TestMergeSecretsBlankPreservesStored(t *testing.T) {
	stored := map[string]any{"client_id": "old", "client_secret": "kept"}
	incoming := map[string]any{"client_id": "new", "client_secret": ""}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_id"] != "new" {
		t.Fatalf("non-secret should update, got %v", out["client_id"])
	}
	if out["client_secret"] != "kept" {
		t.Fatalf("blank secret must preserve stored value, got %v", out["client_secret"])
	}
}

func TestMergeSecretsSentinelPreservesStored(t *testing.T) {
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_secret": secretSentinel}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "kept" {
		t.Fatalf("sentinel must preserve stored value, got %v", out["client_secret"])
	}
}

func TestMergeSecretsNewValueOverwrites(t *testing.T) {
	stored := map[string]any{"client_secret": "old"}
	incoming := map[string]any{"client_secret": "fresh"}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "fresh" {
		t.Fatalf("non-blank secret must overwrite, got %v", out["client_secret"])
	}
}

func TestMergeSecretsStripsIsSetKeys(t *testing.T) {
	// The client may echo back the "<key>__isSet" sidecar; it must never be persisted.
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_id": "abc", "client_secret": "", "client_secret__isSet": true}
	out := mergeSecrets(schema(), stored, incoming)
	if _, present := out["client_secret__isSet"]; present {
		t.Fatal("__isSet sidecar must not be persisted")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run "Redact|MergeSecrets" -v`
Expected: FAIL — `undefined: redactConfig` / `undefined: mergeSecrets` / `undefined: secretSentinel`.

- [ ] **Step 3: Write the implementation**

Create `internal/api/adapters_secrets.go`:
```go
package api

import (
	"strings"

	"github.com/maximusjb/reverb/internal/registry"
)

// secretSentinel is the placeholder returned for a SET secret. The browser never
// receives the real value. Submitting the sentinel (or a blank string) back means
// "keep the stored secret".
const secretSentinel = "••••••••"

// isSetSuffix is appended to a secret field key to carry a boolean indicating
// whether a value is stored, without ever exposing the value itself.
const isSetSuffix = "__isSet"

// secretKeys returns the set of Secret:true field keys from a schema.
func secretKeys(schema registry.ConfigSchema) map[string]bool {
	out := map[string]bool{}
	for _, f := range schema.Fields {
		if f.Secret {
			out[f.Key] = true
		}
	}
	return out
}

// redactConfig copies cfg, removing every Secret:true value and replacing it with a
// parallel "<key>__isSet" boolean. Non-secret fields pass through unchanged. Generic:
// it consults the schema only, never a per-adapter hardcoded list.
func redactConfig(schema registry.ConfigSchema, cfg map[string]any) map[string]any {
	secrets := secretKeys(schema)
	out := map[string]any{}
	for k, v := range cfg {
		if secrets[k] {
			continue // drop the secret value entirely
		}
		out[k] = v
	}
	for key := range secrets {
		_, present := cfg[key]
		set := present && !isBlank(cfg[key])
		out[key+isSetSuffix] = set
	}
	return out
}

// mergeSecrets builds the config to PERSIST. Non-secret fields take the incoming
// value. Secret fields: if incoming is blank or the sentinel, restore from stored
// (preserve-on-blank); otherwise take the new incoming value. Any "<key>__isSet"
// sidecars are stripped so they never reach config_json.
func mergeSecrets(schema registry.ConfigSchema, stored, incoming map[string]any) map[string]any {
	secrets := secretKeys(schema)
	out := map[string]any{}
	for k, v := range incoming {
		if strings.HasSuffix(k, isSetSuffix) {
			continue // never persist the sidecar
		}
		if secrets[k] {
			if isBlank(v) || asString(v) == secretSentinel {
				if sv, ok := stored[k]; ok {
					out[k] = sv // preserve
				}
				continue
			}
		}
		out[k] = v
	}
	// Carry over any stored secret the client omitted entirely (defensive preserve).
	for key := range secrets {
		if _, ok := out[key]; ok {
			continue
		}
		if sv, ok := stored[key]; ok {
			out[key] = sv
		}
	}
	return out
}

func isBlank(v any) bool {
	s, ok := v.(string)
	return ok && s == ""
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/api/ -run "Redact|MergeSecrets" -v`
Expected: PASS (all six subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/api/adapters_secrets.go internal/api/adapters_secrets_test.go
git commit -m "feat(api): generic ConfigSchema-driven secret redaction and preserve-on-blank merge"
```

---

## Task 3: Deps wiring — AdapterStore interface, ConfigDirty flag, server routes

**Files:**
- Modify: `internal/api/server.go`
- Test: `internal/api/config_test.go` (NEW — pending-restart) and a compile-smoke in `adapters_test.go` (Task 4)

**Interfaces:**
- Produces (added to `internal/api`):
  ```go
  // AdapterStore is the persistence slice the adapter/settings handlers need.
  // *db.Queries (from store.Store.Q()) satisfies it directly — no wrapper needed.
  type AdapterStore interface {
      ListAdapterInstances(ctx context.Context) ([]db.AdapterInstance, error)
      GetAdapterInstance(ctx context.Context, id string) (db.AdapterInstance, error)
      CreateAdapterInstance(ctx context.Context, arg db.CreateAdapterInstanceParams) error
      UpdateAdapterInstance(ctx context.Context, arg db.UpdateAdapterInstanceParams) error
      DeleteAdapterInstance(ctx context.Context, id string) error
      GetSetting(ctx context.Context, key string) (string, error)
      UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
  }

  // ConfigDirty tracks whether adapter/settings config changed since startup
  // (restart-to-apply). *atomicDirty (cmd/reverb) satisfies it.
  type ConfigDirty interface {
      Set()
      Dirty() bool
  }
  ```
  - `*db.Queries` already has all seven methods, so `*store.Store`'s `Q()` value (a `*db.Queries`) directly satisfies `AdapterStore` — no wrapper needed beyond passing `st.Q()`.

- [ ] **Step 1: Add the interfaces + Deps fields + routes to server.go**

Edit `internal/api/server.go`. Add `"context"` is already imported and `db` import. Update the import block to include the store db package:
```go
	"github.com/maximusjb/reverb/internal/store/db"
```
Add the interfaces right after the `DownloadManager` interface block:
```go
// AdapterStore is the persistence slice the adapter + settings handlers need.
// *db.Queries (from store.Store.Q()) satisfies it directly.
type AdapterStore interface {
	ListAdapterInstances(ctx context.Context) ([]db.AdapterInstance, error)
	GetAdapterInstance(ctx context.Context, id string) (db.AdapterInstance, error)
	CreateAdapterInstance(ctx context.Context, arg db.CreateAdapterInstanceParams) error
	UpdateAdapterInstance(ctx context.Context, arg db.UpdateAdapterInstanceParams) error
	DeleteAdapterInstance(ctx context.Context, id string) error
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// ConfigDirty tracks whether adapter/settings config changed since startup. The
// restart-to-apply UX reads this so it can show a "Restart to apply" banner.
type ConfigDirty interface {
	Set()
	Dirty() bool
}
```
Add fields to `Deps`:
```go
	Adapters    AdapterStore
	Library     library.LibraryAdapter // (already present)
	Lib         *registry.Registry     // library registry for /adapters/available + /test
	ConfigDirty ConfigDirty
```

> NOTE: `Deps` already has `Library library.LibraryAdapter` (the active adapter) and `Search`/`Downloader` registries. Add a NEW `Lib *registry.Registry` field for the LIBRARY REGISTRY (used to list available library adapters + build a non-persisted one for `/test`). Do not confuse the active `Library` adapter with the `Lib` registry.

Add the routes inside the protected group in `routes()` (after the existing `pr.Get("/adapters/available", ...)`):
```go
			pr.Get("/adapters", s.handleListAdapters)
			pr.Post("/adapters", s.handleCreateAdapter)
			pr.Put("/adapters/{id}", s.handleUpdateAdapter)
			pr.Delete("/adapters/{id}", s.handleDeleteAdapter)
			pr.Post("/adapters/test", s.handleTestAdapter)
			pr.Get("/settings", s.handleGetSettings)
			pr.Put("/settings", s.handlePutSettings)
			pr.Get("/config/pending-restart", s.handlePendingRestart)
```

- [ ] **Step 2: Create the shared test helper (REQUIRED before config_test.go)**

> **NOTE: Tasks 3, 4, and 5 form the adapters-API unit. The shared test helper is created HERE in Task 3 so that every subsequent task's `go test ./internal/api/` compiles at each task boundary. Create the helper first, then config_test.go.**

Create `internal/api/testhelpers_test.go`:
```go
package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maximusjb/reverb/internal/auth"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store"
)

// testDirty is a minimal ConfigDirty for tests.
type testDirty struct{ b atomic.Bool }

func (d *testDirty) Set()        { d.b.Store(true) }
func (d *testDirty) Dirty() bool { return d.b.Load() }

// fakeAdapter is a controllable registry.Plugin with one Secret field.
type fakeAdapter struct {
	typ     string
	name    string
	testErr error
}

func (a *fakeAdapter) Type() string { return a.typ }
func (a *fakeAdapter) Name() string { return a.name }
func (a *fakeAdapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "URL", Type: "string", Required: true},
		{Key: "token", Label: "Token", Type: "string", Required: true, Secret: true},
	}}
}
func (a *fakeAdapter) Init(map[string]any) error            { return nil }
func (a *fakeAdapter) TestConnection(context.Context) error { return a.testErr }

type adapterServerOpts struct {
	dirty   ConfigDirty
	testErr error // controls the fake search adapter's TestConnection
}

// adapterTestServer builds a Server with a temp store, an authed session, and a
// search registry containing a controllable fake adapter named "fake".
func adapterTestServer(t *testing.T, opts adapterServerOpts) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/adapters.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(context.Background())

	searchReg := registry.NewRegistry("search")
	searchReg.Register("fake", func() registry.Plugin {
		return &fakeAdapter{typ: "search", name: "fake", testErr: opts.testErr}
	})

	srv := NewServer(Deps{
		Auth:        authSvc,
		Adapters:    st.Q(),
		Search:      searchReg,
		Downloader:  registry.NewRegistry("downloader"),
		Lib:         registry.NewRegistry("library"),
		ConfigDirty: opts.dirty,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

// do fires an authenticated HTTP request against the server and returns the recorder.
func do(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = bytes.NewBufferString("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
```

- [ ] **Step 3: Write the failing pending-restart test**

Create `internal/api/config_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPendingRestartReflectsFlag(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})

	get := func() bool {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil)
		req.AddCookie(cookie)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var body struct {
			PendingRestart bool `json:"pendingRestart"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.PendingRestart
	}

	if get() {
		t.Fatal("should start clean")
	}
	dirty.Set()
	if !get() {
		t.Fatal("should be dirty after Set()")
	}
}

func TestPendingRestartNilSafe(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: nil})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
```

- [ ] **Step 4: Write the config handler**

Create `internal/api/config.go`:
```go
package api

import "net/http"

// handlePendingRestart reports whether any adapter/settings change has been made
// since startup. With restart-to-apply (M4a) the UI shows a banner when true.
func (s *Server) handlePendingRestart(w http.ResponseWriter, r *http.Request) {
	dirty := false
	if s.deps.ConfigDirty != nil {
		dirty = s.deps.ConfigDirty.Dirty()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"pendingRestart": dirty})
}
```

- [ ] **Step 5: Verify the package compiles (handlers for adapters/settings land in Tasks 4–6)**

Routes reference `s.handleListAdapters` etc. which do not exist yet — they are added in Tasks 4–6. To keep the package compiling between tasks, add THIN stubs now in `internal/api/adapters.go` and `internal/api/settings.go` that 501 (replaced fully in Tasks 4–6):
```go
// internal/api/adapters.go (stub — fully implemented in Tasks 4-5)
package api

import "net/http"

func (s *Server) handleListAdapters(w http.ResponseWriter, r *http.Request)   { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
func (s *Server) handleCreateAdapter(w http.ResponseWriter, r *http.Request)  { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
func (s *Server) handleUpdateAdapter(w http.ResponseWriter, r *http.Request)  { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
func (s *Server) handleDeleteAdapter(w http.ResponseWriter, r *http.Request)  { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
func (s *Server) handleTestAdapter(w http.ResponseWriter, r *http.Request)    { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
```
```go
// internal/api/settings.go (stub — fully implemented in Task 6)
package api

import "net/http"

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"}) }
```

Run: `go build ./internal/api/`
Expected: builds cleanly. `testhelpers_test.go` (with `adapterTestServer`, `do`, `fakeAdapter`, `testDirty`) is already present, so `config_test.go` also compiles: `go test ./internal/api/ -run PendingRestart -v` should PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go internal/api/config.go internal/api/adapters.go internal/api/settings.go internal/api/config_test.go internal/api/testhelpers_test.go
git commit -m "feat(api): Deps adapter store + config-dirty flag, pending-restart route, handler stubs"
```

---

## Task 4: Adapters REST API — list (redacted), create, update (secret-preserving), delete

**Files:**
- Modify: `internal/api/adapters.go` (replace the list/create/update/delete stubs with full impls)
- Modify: `internal/api/handlers.go` (extend `handleAdaptersAvailable` to also include the Library registry)
- Test: `internal/api/adapters_test.go` (NEW)

**Interfaces:**
- Consumes: `s.deps.Adapters` (AdapterStore), `s.deps.Search`/`s.deps.Downloader`/`s.deps.Lib` registries, `s.deps.ConfigDirty`, `redactConfig`/`mergeSecrets` (Task 2), `uuid`, `chi.URLParam`.
- Produces (HTTP):
  ```
  GET    /api/v1/adapters            → [{id,type,name,enabled,priority,config(redacted)}], 200
  POST   /api/v1/adapters            → {type,name,config,enabled,priority} → created instance (redacted), 201; flips dirty
  PUT    /api/v1/adapters/{id}       → {name?,config,enabled,priority} (secret-preserving) → updated (redacted), 200; flips dirty
  DELETE /api/v1/adapters/{id}       → {ok:true}, 200; flips dirty
  ```
- DTO:
  ```go
  type adapterInstanceDTO struct {
      ID       string         `json:"id"`
      Type     string         `json:"type"`
      Name     string         `json:"name"`
      Enabled  bool           `json:"enabled"`
      Priority int            `json:"priority"`
      Config   map[string]any `json:"config"` // secrets redacted via redactConfig
  }
  ```

- [ ] **Step 1: Write the failing tests**

> `adapterTestServer`, `adapterServerOpts`, `do`, `fakeAdapter`, and `testDirty` are already defined in `internal/api/testhelpers_test.go` (created in Task 3 Step 2). Do NOT redefine them here — the package will have duplicate-definition errors if you do. Just create `adapters_test.go` with the test functions below.

Create `internal/api/adapters_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateThenListRedactsSecret(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})

	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"shh"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	if !dirty.Dirty() {
		t.Fatal("create must flip the config-dirty flag")
	}

	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list []adapterInstanceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 instance, got %d", len(list))
	}
	cfg := list[0].Config
	if cfg["url"] != "http://x" {
		t.Fatalf("non-secret should be visible, got %v", cfg["url"])
	}
	if _, present := cfg["token"]; present {
		t.Fatalf("secret VALUE must NOT be returned, got %v", cfg["token"])
	}
	if cfg["token__isSet"] != true {
		t.Fatalf("expected token__isSet=true, got %v", cfg["token__isSet"])
	}
}

func TestUpdatePreservesSecretWhenBlank(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"orig"}}`)
	var created adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Update with a blank token → must preserve "orig".
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/adapters/"+created.ID,
		`{"name":"fake","enabled":true,"priority":3,"config":{"url":"http://y","token":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", rec.Code, rec.Body.String())
	}

	// Read the raw stored config_json via the store to assert the secret survived.
	inst, err := getStoredInstance(t, srv, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	_ = json.Unmarshal([]byte(inst), &stored)
	if stored["token"] != "orig" {
		t.Fatalf("blank update must preserve stored secret, got %v", stored["token"])
	}
	if stored["url"] != "http://y" {
		t.Fatalf("non-secret should update, got %v", stored["url"])
	}
}

func TestUpdateNewSecretOverwrites(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"config":{"url":"http://x","token":"orig"}}`)
	var created adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/adapters/"+created.ID,
		`{"name":"fake","enabled":true,"config":{"url":"http://x","token":"changed"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	inst, _ := getStoredInstance(t, srv, created.ID)
	var stored map[string]any
	_ = json.Unmarshal([]byte(inst), &stored)
	if stored["token"] != "changed" {
		t.Fatalf("new secret must overwrite, got %v", stored["token"])
	}
}

func TestDeleteAdapter(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","config":{"url":"http://x","token":"t"}}`)
	var created adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = do(t, srv, cookie, http.MethodDelete, "/api/v1/adapters/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	var list []adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("want 0 after delete, got %d", len(list))
	}
}

func TestAdaptersRequireAuth(t *testing.T) {
	srv, _ := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/adapters", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// getStoredInstance reads the RAW config_json for an instance from the server's
// store (test helper). It uses the AdapterStore on Deps via a small accessor.
func getStoredInstance(t *testing.T, srv *Server, id string) (string, error) {
	t.Helper()
	inst, err := srv.deps.Adapters.GetAdapterInstance(context.Background(), id)
	if err != nil {
		return "", err
	}
	return inst.ConfigJson, nil
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run "Create|Update|Delete|AdaptersRequireAuth" -v`
Expected: FAIL — the stub handlers return 501 (`undefined: adapterInstanceDTO` initially too).

- [ ] **Step 3: Implement the adapters handlers**

Replace the stub `internal/api/adapters.go` with:
```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store/db"
)

// adapterInstanceDTO is the browser-facing shape of a configured adapter instance.
// Config has Secret:true fields redacted (value removed, "<key>__isSet" boolean added).
type adapterInstanceDTO struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

// createAdapterBody / updateAdapterBody are the request DTOs.
type createAdapterBody struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

type updateAdapterBody struct {
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

// registries returns the three registries in a stable order for lookup.
func (s *Server) registries() []*registry.Registry {
	return []*registry.Registry{s.deps.Lib, s.deps.Search, s.deps.Downloader}
}

// schemaFor finds the ConfigSchema for an adapter name across all registries.
// Returns an empty schema if the name is not registered (redaction still safe).
func (s *Server) schemaFor(name string) registry.ConfigSchema {
	for _, reg := range s.registries() {
		if reg == nil {
			continue
		}
		for _, n := range reg.Names() {
			if n != name {
				continue
			}
			if p, err := reg.Create(n); err == nil {
				return p.ConfigSchema()
			}
		}
	}
	return registry.ConfigSchema{}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// toDTO converts a stored row into the redacted browser DTO.
func (s *Server) toDTO(inst db.AdapterInstance) adapterInstanceDTO {
	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		_ = json.Unmarshal([]byte(inst.ConfigJson), &cfg)
	}
	return adapterInstanceDTO{
		ID:       inst.ID,
		Type:     inst.Type,
		Name:     inst.Name,
		Enabled:  inst.Enabled == 1,
		Priority: int(inst.Priority),
		Config:   redactConfig(s.schemaFor(inst.Name), cfg),
	}
}

func (s *Server) markDirty() {
	if s.deps.ConfigDirty != nil {
		s.deps.ConfigDirty.Set()
	}
}

func (s *Server) handleListAdapters(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusOK, []adapterInstanceDTO{})
		return
	}
	rows, err := s.deps.Adapters.ListAdapterInstances(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list adapters"})
		return
	}
	out := make([]adapterInstanceDTO, 0, len(rows))
	for _, inst := range rows {
		out = append(out, s.toDTO(inst))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAdapter(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	var body createAdapterBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.Type == "" || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type and name are required"})
		return
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	// New instance: no stored secrets to preserve; just strip any __isSet sidecars.
	persist := mergeSecrets(s.schemaFor(body.Name), map[string]any{}, body.Config)
	cfgJSON, _ := json.Marshal(persist)
	id := uuid.NewString()
	if err := s.deps.Adapters.CreateAdapterInstance(r.Context(), db.CreateAdapterInstanceParams{
		ID: id, Type: body.Type, Name: body.Name,
		Enabled: boolToInt(body.Enabled), Priority: int64(body.Priority), ConfigJson: string(cfgJSON),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create adapter"})
		return
	}
	s.markDirty()
	inst, _ := s.deps.Adapters.GetAdapterInstance(r.Context(), id)
	writeJSONPending(w, http.StatusCreated, s.toDTO(inst), s.dirtyNow())
}

func (s *Server) handleUpdateAdapter(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := s.deps.Adapters.GetAdapterInstance(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "adapter not found"})
		return
	}
	var body updateAdapterBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	stored := map[string]any{}
	if existing.ConfigJson != "" {
		_ = json.Unmarshal([]byte(existing.ConfigJson), &stored)
	}
	name := body.Name
	if name == "" {
		name = existing.Name
	}
	persist := mergeSecrets(s.schemaFor(name), stored, body.Config)
	cfgJSON, _ := json.Marshal(persist)
	if err := s.deps.Adapters.UpdateAdapterInstance(r.Context(), db.UpdateAdapterInstanceParams{
		Name: name, Enabled: boolToInt(body.Enabled), Priority: int64(body.Priority),
		ConfigJson: string(cfgJSON), ID: id,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update adapter"})
		return
	}
	s.markDirty()
	inst, _ := s.deps.Adapters.GetAdapterInstance(r.Context(), id)
	writeJSONPending(w, http.StatusOK, s.toDTO(inst), s.dirtyNow())
}

func (s *Server) handleDeleteAdapter(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.deps.Adapters.DeleteAdapterInstance(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not delete adapter"})
		return
	}
	s.markDirty()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pendingRestart": s.dirtyNow()})
}

func (s *Server) dirtyNow() bool {
	return s.deps.ConfigDirty != nil && s.deps.ConfigDirty.Dirty()
}

// handleTestAdapter is a temporary stub until Task 5 replaces it with the real
// implementation. It is included here so that `go build ./internal/api/` succeeds
// at the Task 4 boundary (the /adapters/test route is registered in server.go).
// Task 5 Step 3 will overwrite this function with the real instantiate→Init→TestConnection logic.
func (s *Server) handleTestAdapter(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented yet"})
}
```

> **Compile invariant at Task 4 boundary:** after completing Task 4, `go build ./internal/api/` must succeed. The stub `handleTestAdapter` above satisfies the route registered in server.go. Task 5 replaces it with the real implementation.

Add `writeJSONPending` to `internal/api/handlers.go` (near `writeJSON`):
```go
// writeJSONPending wraps a payload with the restart-to-apply flag so the client can
// surface the "Restart to apply" banner immediately after a mutation.
func writeJSONPending(w http.ResponseWriter, status int, v any, pending bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Data           any  `json:"data"`
		PendingRestart bool `json:"pendingRestart"`
	}{Data: v, PendingRestart: pending})
}
```

> **DTO shape note:** create/update responses are `{data:<dto>, pendingRestart:bool}`. The test `TestCreateThenListRedactsSecret` unmarshals the LIST (`[]adapterInstanceDTO`) directly — list is NOT wrapped. The create response IS wrapped; the test reads `created.ID` from the wrapped create body. Adjust the create test parse to unmarshal `{data,pendingRestart}` then read `.Data` — UPDATE the test helper accordingly: in `adapters_test.go`, replace the two `var created adapterInstanceDTO; _ = json.Unmarshal(rec.Body.Bytes(), &created)` create-parse sites with:
> ```go
> var wrap struct{ Data adapterInstanceDTO `json:"data"` }
> _ = json.Unmarshal(rec.Body.Bytes(), &wrap)
> created := wrap.Data
> ```
> Apply this in `TestUpdatePreservesSecretWhenBlank`, `TestUpdateNewSecretOverwrites`, and `TestDeleteAdapter`. `TestCreateThenListRedactsSecret` checks the create succeeded (201 + dirty) then asserts via the LIST, so it needs no create-body parse.

- [ ] **Step 4: Extend `handleAdaptersAvailable` to include the Library registry**

In `internal/api/handlers.go`, change the available loop to iterate all three registries:
```go
func (s *Server) handleAdaptersAvailable(w http.ResponseWriter, r *http.Request) {
	out := make([]adapterInfo, 0)
	for _, reg := range []*registry.Registry{s.deps.Lib, s.deps.Search, s.deps.Downloader} {
		if reg == nil {
			continue
		}
		for _, name := range reg.Names() {
			p, err := reg.Create(name)
			if err != nil {
				continue
			}
			out = append(out, adapterInfo{
				Type:         p.Type(),
				Name:         p.Name(),
				ConfigSchema: p.ConfigSchema(),
				Capabilities: registry.DescribeCapabilities(p),
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/api/ -run "Create|Update|Delete|AdaptersRequireAuth|PendingRestart" -v`
Expected: PASS. Also run the full api package to ensure no regressions: `go test ./internal/api/`.
Expected: PASS (existing M0–M3 api tests + new).

- [ ] **Step 6: Commit**

```bash
git add internal/api/adapters.go internal/api/adapters_test.go internal/api/handlers.go internal/api/config_test.go
git commit -m "feat(api): adapter instances CRUD with secret redaction + preserve-on-blank + pending-restart"
```

---

## Task 5: `/adapters/test` — non-persisted instantiate → TestConnection (timeout-bounded)

**Files:**
- Modify: `internal/api/adapters.go` (replace the `handleTestAdapter` stub)
- Test: `internal/api/adapters_test.go` (add test ok/error cases)

**Interfaces:**
- Consumes: `s.deps.Lib`/`s.deps.Search`/`s.deps.Downloader` registries, `registry.Create(name)` → `Init(config + env secret overlay)` → `TestConnection(ctx)`. No persistence.
- Produces (HTTP):
  ```
  POST /api/v1/adapters/test  {name, config}  → {ok:bool, error?:string}, 200
  ```
  - Builds a NON-persisted adapter via the registry, `Init`s with the submitted config (secrets included — the client sends them only for the live test; nothing is stored), calls `TestConnection` under a 10s context, returns `{ok:true}` or `{ok:false, error:"..."}`. Never 5xx for a connection failure (that is a normal `ok:false` result); 400 only for a malformed request or unknown adapter name.

> **Secret overlay note:** for `/test`, the client sends actual secret values typed into the form. If the secret field is left blank AND the instance already exists, the client should NOT call /test with a blank secret (the form's Test button is for verifying what is typed). The backend additionally applies the same env override the composition root uses, so a test honors `REVERB_*` secrets when the form omits them. M4a keeps this simple: the handler applies env overlays for the known secret env vars by adapter name (mirrors the wiring files). For unknown adapters it just uses the submitted config.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/adapters_test.go`:
```go
func TestTestAdapterOK(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}, testErr: nil})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"fake","config":{"url":"http://x","token":"t"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.OK {
		t.Fatalf("expected ok=true, got %+v", body)
	}
}

func TestTestAdapterError(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}, testErr: errFakeConn})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"fake","config":{"url":"http://x","token":"t"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (a connection failure is still a 200 ok:false)", rec.Code)
	}
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.OK || body.Error == "" {
		t.Fatalf("expected ok=false with error, got %+v", body)
	}
}

func TestTestAdapterUnknownName(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"nope","config":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown adapter", rec.Code)
	}
}
```
Add at the top of `adapters_test.go` (after imports) the sentinel error:
```go
import "errors"

var errFakeConn = errors.New("connection refused")
```
(Merge the `errors` import into the existing import block rather than a second `import` line.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run "TestAdapter" -v`
Expected: FAIL — `handleTestAdapter` still 501.

- [ ] **Step 3: Implement `handleTestAdapter`**

Replace the stub `handleTestAdapter` in `internal/api/adapters.go` (remove the stub; add the real one + an env-overlay helper). Add imports `"context"`, `"os"`, `"time"`:
```go
type testAdapterBody struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config"`
}

// createUnregistered finds and instantiates a NON-persisted adapter by name across
// all registries. Returns (nil, false) if the name is not registered.
func (s *Server) createUnregistered(name string) (registry.Plugin, bool) {
	for _, reg := range s.registries() {
		if reg == nil {
			continue
		}
		for _, n := range reg.Names() {
			if n == name {
				if p, err := reg.Create(n); err == nil {
					return p, true
				}
			}
		}
	}
	return nil, false
}

// overlayEnvSecrets applies the same env secret overrides the composition root uses,
// so a Test honors REVERB_* secrets when the form omits them. Mirrors *_wiring.go.
func overlayEnvSecrets(name string, cfg map[string]any) {
	switch name {
	case "subsonic":
		if v := os.Getenv("REVERB_LIBRARY_PASSWORD"); v != "" {
			cfg["password"] = v
		}
	case "spotify":
		if v := os.Getenv("REVERB_SPOTIFY_CLIENT_SECRET"); v != "" {
			cfg["client_secret"] = v
		}
	case "spotdl":
		if v := os.Getenv("REVERB_SPOTDL_PATH"); v != "" {
			cfg["binary_path"] = v
		}
		if v := os.Getenv("REVERB_DOWNLOAD_DIR"); v != "" {
			cfg["output_dir"] = v
		}
	}
}

func (s *Server) handleTestAdapter(w http.ResponseWriter, r *http.Request) {
	var body testAdapterBody
	if err := decode(r, &body); err != nil || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	plugin, ok := s.createUnregistered(body.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown adapter: " + body.Name})
		return
	}
	cfg := body.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	// Strip any __isSet sidecars the client may echo; never feed them to Init.
	for k := range cfg {
		if len(k) > len(isSetSuffix) && k[len(k)-len(isSetSuffix):] == isSetSuffix {
			delete(cfg, k)
		}
	}
	overlayEnvSecrets(body.Name, cfg)

	if err := plugin.Init(cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := plugin.TestConnection(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
```
Remove the temporary stub `handleTestAdapter` that was added at the bottom of `adapters.go` in Task 4 (the one that returns 501 "not implemented yet") and replace it with the real implementation above. Ensure only ONE definition of `handleTestAdapter` exists in the package.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/ -run "TestAdapter" -v`
Expected: PASS (ok / error / unknown).
Run the full package: `go test ./internal/api/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/adapters.go internal/api/adapters_test.go
git commit -m "feat(api): /adapters/test instantiates non-persisted adapter and runs TestConnection"
```

---

## Task 6: Settings REST API — `GET`/`PUT /settings` (accent_color, dynamic_background)

**Files:**
- Modify: `internal/api/settings.go` (replace stubs)
- Test: `internal/api/settings_test.go` (NEW)

**Interfaces:**
- Consumes: `s.deps.Adapters` (for `GetSetting`/`UpsertSetting`).
- Produces (HTTP):
  ```
  GET /api/v1/settings  → {accentColor:"#F0354B", dynamicBackground:true}, 200 (defaults when unset)
  PUT /api/v1/settings  {accentColor?, dynamicBackground?} → echoes the new settings, 200
  ```
  - Setting keys persisted in `settings`: `accent_color` (hex string, default `#F0354B`), `dynamic_background` (`"true"`/`"false"`, default `true`). These are non-secret and do NOT flip the config-dirty flag (they apply live in the SPA).

- [ ] **Step 1: Write the failing test**

Create `internal/api/settings_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGetSettingsDefaults(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		AccentColor       string `json:"accentColor"`
		DynamicBackground bool   `json:"dynamicBackground"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccentColor != "#F0354B" {
		t.Fatalf("default accent should be #F0354B, got %q", body.AccentColor)
	}
	if !body.DynamicBackground {
		t.Fatal("dynamic_background should default to true")
	}
}

func TestPutThenGetSettings(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"accentColor":"#00FF88","dynamicBackground":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	var body struct {
		AccentColor       string `json:"accentColor"`
		DynamicBackground bool   `json:"dynamicBackground"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccentColor != "#00FF88" || body.DynamicBackground {
		t.Fatalf("round trip failed: %+v", body)
	}
}

func TestSettingsRequireAuth(t *testing.T) {
	srv, _ := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := newRec()
	srv.Handler().ServeHTTP(rec, newReq(http.MethodGet, "/api/v1/settings", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
```
Add tiny helpers to `settings_test.go` (or reuse `do`; the auth test needs NO cookie):
```go
import (
	"bytes"
	"net/http/httptest"
)

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }
func newReq(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, bytes.NewBufferString(body))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run Settings -v`
Expected: FAIL — settings handlers still 501.

- [ ] **Step 3: Implement the settings handlers**

Replace the stub `internal/api/settings.go` with:
```go
package api

import (
	"net/http"

	"github.com/maximusjb/reverb/internal/store/db"
)

const (
	keyAccentColor       = "accent_color"
	keyDynamicBackground = "dynamic_background"
	defaultAccentColor   = "#F0354B"
)

type settingsDTO struct {
	AccentColor       string `json:"accentColor"`
	DynamicBackground bool   `json:"dynamicBackground"`
}

func (s *Server) currentSettings(r *http.Request) settingsDTO {
	out := settingsDTO{AccentColor: defaultAccentColor, DynamicBackground: true}
	if s.deps.Adapters == nil {
		return out
	}
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyAccentColor); err == nil && v != "" {
		out.AccentColor = v
	}
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyDynamicBackground); err == nil {
		out.DynamicBackground = v != "false"
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentSettings(r))
}

// putSettingsBody uses pointers so an omitted field is left unchanged.
type putSettingsBody struct {
	AccentColor       *string `json:"accentColor"`
	DynamicBackground *bool   `json:"dynamicBackground"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	var body putSettingsBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.AccentColor != nil {
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyAccentColor, Value: *body.AccentColor}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	if body.DynamicBackground != nil {
		v := "true"
		if !*body.DynamicBackground {
			v = "false"
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyDynamicBackground, Value: v}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	writeJSON(w, http.StatusOK, s.currentSettings(r))
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/ -run Settings -v`
Expected: PASS.
Run the full package: `go test ./internal/api/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/settings.go internal/api/settings_test.go
git commit -m "feat(api): GET/PUT settings for accent_color and dynamic_background"
```

---

## Task 7: Composition root — dirty flag + Deps wiring

**Files:**
- Create: `cmd/reverb/config_dirty.go`
- Modify: `cmd/reverb/main.go`

**Interfaces:**
- Produces:
  ```go
  type atomicDirty struct{ b atomic.Bool }
  func (d *atomicDirty) Set()        { d.b.Store(true) }
  func (d *atomicDirty) Dirty() bool { return d.b.Load() }
  ```
  - `*atomicDirty` satisfies `api.ConfigDirty`. Wired into `api.Deps{ConfigDirty: ...}`, plus `Adapters: st.Q()` and `Lib: libraryReg`.

- [ ] **Step 1: Write the dirty flag**

Create `cmd/reverb/config_dirty.go`:
```go
package main

import "sync/atomic"

// atomicDirty is the restart-to-apply flag: any adapter/settings mutation flips it,
// and GET /config/pending-restart reports it so the UI shows a "Restart to apply"
// banner. M4a applies adapter changes on the next process start (no hot-reload).
type atomicDirty struct{ b atomic.Bool }

func (d *atomicDirty) Set()        { d.b.Store(true) }
func (d *atomicDirty) Dirty() bool { return d.b.Load() }
```

- [ ] **Step 2: Wire it into main.go**

In `cmd/reverb/main.go`, after `bus := events.New()` (or anywhere before building `deps`), add:
```go
	dirty := &atomicDirty{}
```
Then extend the `deps` literal:
```go
	deps := api.Deps{
		Auth:        authSvc,
		Library:     libAdapter,
		Lib:         libraryReg,
		Search:      searchReg,
		Downloader:  downloaderReg,
		Adapters:    st.Q(),
		Events:      bus,
		ConfigDirty: dirty,
		Dev:         cfg.Dev,
	}
```
(Keep the existing conditional assignments of `deps.SearchAggregator` and `deps.Downloads`.)

- [ ] **Step 3: Build + verify all call sites compile**

Run: `go build ./cmd/... ./internal/...`
Expected: builds cleanly. `*db.Queries` (returned by `st.Q()`) satisfies `api.AdapterStore` (compile-time check happens here).

Then run the whole Go suite:
```bash
go test ./cmd/... ./internal/...
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/reverb/config_dirty.go cmd/reverb/main.go
git commit -m "feat(cmd): wire adapter store, library registry, and restart-to-apply dirty flag into API"
```

---

## Task 8: Frontend API clients — `api.ts` put/del + `adaptersApi.ts` + `settingsApi.ts`

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/lib/adaptersApi.ts`
- Create: `web/src/lib/settingsApi.ts`
- Test: `web/src/lib/adaptersApi.test.tsx`, `web/src/lib/settingsApi.test.ts`

**Interfaces:**
- Produces (TS):
  ```ts
  // api.ts gains:
  put: <T>(p: string, b?: unknown) => Promise<T>
  del: <T>(p: string) => Promise<T>

  // adaptersApi.ts:
  interface ConfigField { key: string; label: string; type: string; required: boolean; secret: boolean }
  interface ConfigSchema { fields: ConfigField[] }
  interface AvailableAdapter { type: string; name: string; configSchema: ConfigSchema; capabilities: string[] }
  interface AdapterInstance { id: string; type: string; name: string; enabled: boolean; priority: number; config: Record<string, unknown> }
  interface TestResult { ok: boolean; error?: string }
  function listAvailable(): Promise<AvailableAdapter[]>
  function listAdapters(): Promise<AdapterInstance[]>
  function createAdapter(b): Promise<{data: AdapterInstance; pendingRestart: boolean}>
  function updateAdapter(id, b): Promise<{data: AdapterInstance; pendingRestart: boolean}>
  function deleteAdapter(id): Promise<{ok: boolean; pendingRestart: boolean}>
  function testAdapter(name, config): Promise<TestResult>
  function getPendingRestart(): Promise<{pendingRestart: boolean}>
  // hooks: useAvailableAdapters, useAdapters, usePendingRestart

  // settingsApi.ts:
  interface AppSettings { accentColor: string; dynamicBackground: boolean }
  function getSettings(): Promise<AppSettings>
  function putSettings(b: Partial<AppSettings>): Promise<AppSettings>
  function hexToRgbChannels(hex: string): string   // "#F0354B" -> "240 53 75"
  function applyAccent(hex: string): void           // sets --color-accent on <html>
  // hooks: useSettings
  ```

- [ ] **Step 1: Add put/del to api.ts**

Edit `web/src/lib/api.ts` — extend the exported `api` object:
```ts
export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}
```

- [ ] **Step 2: Write the failing adaptersApi test**

Create `web/src/lib/adaptersApi.test.tsx`:
```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { listAdapters, createAdapter, testAdapter, deleteAdapter } from './adaptersApi'

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset()
})
afterEach(() => {
  vi.unstubAllGlobals()
})

function ok(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(body)) } as Response)
}

describe('adaptersApi', () => {
  it('listAdapters GETs /adapters', async () => {
    fetchMock.mockReturnValue(ok([{ id: 'a1', type: 'search', name: 'spotify', enabled: true, priority: 0, config: { client_id: 'x', client_secret__isSet: true } }]))
    const out = await listAdapters()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters', expect.objectContaining({ method: 'GET' }))
    expect(out[0].name).toBe('spotify')
    expect(out[0].config.client_secret__isSet).toBe(true)
  })

  it('createAdapter POSTs and returns wrapped data', async () => {
    fetchMock.mockReturnValue(ok({ data: { id: 'a2', type: 'search', name: 'spotify', enabled: true, priority: 0, config: {} }, pendingRestart: true }))
    const res = await createAdapter({ type: 'search', name: 'spotify', enabled: true, priority: 0, config: { client_id: 'x', client_secret: 'shh' } })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters', expect.objectContaining({ method: 'POST' }))
    expect(res.pendingRestart).toBe(true)
    expect(res.data.id).toBe('a2')
  })

  it('testAdapter POSTs /adapters/test', async () => {
    fetchMock.mockReturnValue(ok({ ok: false, error: 'connection refused' }))
    const res = await testAdapter('spotify', { client_id: 'x', client_secret: 'shh' })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters/test', expect.objectContaining({ method: 'POST' }))
    expect(res.ok).toBe(false)
    expect(res.error).toBe('connection refused')
  })

  it('deleteAdapter DELETEs /adapters/:id', async () => {
    fetchMock.mockReturnValue(ok({ ok: true, pendingRestart: true }))
    const res = await deleteAdapter('a1')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters/a1', expect.objectContaining({ method: 'DELETE' }))
    expect(res.ok).toBe(true)
  })
})
```

- [ ] **Step 3: Write adaptersApi.ts**

Create `web/src/lib/adaptersApi.ts`:
```ts
import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface ConfigField {
  key: string
  label: string
  type: string
  required: boolean
  secret: boolean
}
export interface ConfigSchema {
  fields: ConfigField[]
}
export interface AvailableAdapter {
  type: string
  name: string
  configSchema: ConfigSchema
  capabilities: string[]
}
export interface AdapterInstance {
  id: string
  type: string
  name: string
  enabled: boolean
  priority: number
  config: Record<string, unknown>
}
export interface TestResult {
  ok: boolean
  error?: string
}
export interface CreateAdapterReq {
  type: string
  name: string
  enabled: boolean
  priority: number
  config: Record<string, unknown>
}
export interface UpdateAdapterReq {
  name: string
  enabled: boolean
  priority: number
  config: Record<string, unknown>
}
interface Wrapped<T> {
  data: T
  pendingRestart: boolean
}

export const SECRET_SENTINEL = '••••••••'

export function listAvailable(): Promise<AvailableAdapter[]> {
  return api.get<AvailableAdapter[]>('/adapters/available')
}
export function listAdapters(): Promise<AdapterInstance[]> {
  return api.get<AdapterInstance[]>('/adapters')
}
export function createAdapter(b: CreateAdapterReq): Promise<Wrapped<AdapterInstance>> {
  return api.post<Wrapped<AdapterInstance>>('/adapters', b)
}
export function updateAdapter(id: string, b: UpdateAdapterReq): Promise<Wrapped<AdapterInstance>> {
  return api.put<Wrapped<AdapterInstance>>(`/adapters/${encodeURIComponent(id)}`, b)
}
export function deleteAdapter(id: string): Promise<{ ok: boolean; pendingRestart: boolean }> {
  return api.del<{ ok: boolean; pendingRestart: boolean }>(`/adapters/${encodeURIComponent(id)}`)
}
export function testAdapter(name: string, config: Record<string, unknown>): Promise<TestResult> {
  return api.post<TestResult>('/adapters/test', { name, config })
}
export function getPendingRestart(): Promise<{ pendingRestart: boolean }> {
  return api.get<{ pendingRestart: boolean }>('/config/pending-restart')
}

export function useAvailableAdapters() {
  return useQuery({ queryKey: ['adapters', 'available'], queryFn: listAvailable })
}
export function useAdapters() {
  return useQuery({ queryKey: ['adapters', 'list'], queryFn: listAdapters })
}
export function usePendingRestart() {
  return useQuery({ queryKey: ['config', 'pending-restart'], queryFn: getPendingRestart })
}
```

- [ ] **Step 4: Write the failing settingsApi test**

Create `web/src/lib/settingsApi.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { hexToRgbChannels, applyAccent, getSettings } from './settingsApi'

describe('hexToRgbChannels', () => {
  it('converts #F0354B to space-separated RGB channels', () => {
    expect(hexToRgbChannels('#F0354B')).toBe('240 53 75')
  })
  it('handles a hex without the leading #', () => {
    expect(hexToRgbChannels('00FF88')).toBe('0 255 136')
  })
  it('returns the default red channels for an invalid hex', () => {
    expect(hexToRgbChannels('nope')).toBe('240 53 75')
  })
})

describe('applyAccent', () => {
  it('sets the --color-accent CSS var on <html>', () => {
    applyAccent('#00FF88')
    expect(document.documentElement.style.getPropertyValue('--color-accent')).toBe('0 255 136')
  })
})

describe('getSettings', () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockReset()
  })
  afterEach(() => vi.unstubAllGlobals())
  it('GETs /settings', async () => {
    fetchMock.mockReturnValue(
      Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify({ accentColor: '#F0354B', dynamicBackground: true })) } as Response),
    )
    const s = await getSettings()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/settings', expect.objectContaining({ method: 'GET' }))
    expect(s.accentColor).toBe('#F0354B')
  })
})
```

- [ ] **Step 5: Write settingsApi.ts**

Create `web/src/lib/settingsApi.ts`:
```ts
import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface AppSettings {
  accentColor: string
  dynamicBackground: boolean
}

const DEFAULT_ACCENT_CHANNELS = '240 53 75' // #F0354B

// hexToRgbChannels converts "#RRGGBB" (or "RRGGBB") to "r g b" space-separated
// channels for the --color-accent CSS custom property. Falls back to the default
// red channels for any malformed input.
export function hexToRgbChannels(hex: string): string {
  const m = /^#?([0-9a-fA-F]{6})$/.exec(hex.trim())
  if (!m) return DEFAULT_ACCENT_CHANNELS
  const n = parseInt(m[1], 16)
  const r = (n >> 16) & 0xff
  const g = (n >> 8) & 0xff
  const b = n & 0xff
  return `${r} ${g} ${b}`
}

// applyAccent writes the accent color into the --color-accent CSS var live, so the
// whole app (Tailwind `accent` references rgb(var(--color-accent) / a)) re-themes.
export function applyAccent(hex: string): void {
  document.documentElement.style.setProperty('--color-accent', hexToRgbChannels(hex))
}

export function getSettings(): Promise<AppSettings> {
  return api.get<AppSettings>('/settings')
}
export function putSettings(b: Partial<AppSettings>): Promise<AppSettings> {
  return api.put<AppSettings>('/settings', b)
}

export function useSettings() {
  return useQuery({ queryKey: ['settings'], queryFn: getSettings })
}
```

- [ ] **Step 6: Run the frontend tests + typecheck**

Run: `cd web && npm run test -- adaptersApi settingsApi`
Expected: PASS.
Run: `cd web && npm run build`
Expected: typechecks + builds cleanly.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/api.ts web/src/lib/adaptersApi.ts web/src/lib/adaptersApi.test.tsx web/src/lib/settingsApi.ts web/src/lib/settingsApi.test.ts
git commit -m "feat(web): adapters + settings API clients, accent hex→rgb helper, put/del fetch"
```

---

## Task 9: `AdapterForm` — ConfigSchema-driven form + Test Connection

**Files:**
- Create: `web/src/components/AdapterForm.tsx`
- Test: `web/src/components/AdapterForm.test.tsx`

**Interfaces:**
- Consumes: `ConfigSchema`, `ConfigField`, `testAdapter`, `SECRET_SENTINEL` from `adaptersApi`.
- Produces:
  ```ts
  interface AdapterFormProps {
    name: string                              // adapter name (e.g. "spotify")
    schema: ConfigSchema
    initial?: Record<string, unknown>         // existing config (redacted: secrets carry "<key>__isSet")
    submitLabel?: string                      // default "Save"
    onSubmit: (config: Record<string, unknown>) => void | Promise<void>
  }
  export function AdapterForm(props: AdapterFormProps): JSX.Element
  ```
  - Renders one input per `schema.fields`: `type:"bool"` → checkbox; `type:"number"` → number input; `secret:true` → password input; else text input.
  - For a secret field whose `initial["<key>__isSet"]` is true, the field renders empty with placeholder "Leave blank to keep current value" (the value is never sent down). Submitting blank → the field is sent as `""` (backend preserves).
  - The **Test Connection** button collects the current field values and calls `testAdapter(name, values)`, showing `✓ Connection OK` or `✗ <error>`.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/AdapterForm.test.tsx`:
```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AdapterForm } from './AdapterForm'
import type { ConfigSchema } from '../lib/adaptersApi'

vi.mock('../lib/adaptersApi', async (orig) => {
  const actual = await orig<typeof import('../lib/adaptersApi')>()
  return { ...actual, testAdapter: vi.fn() }
})
import { testAdapter } from '../lib/adaptersApi'

const schema: ConfigSchema = {
  fields: [
    { key: 'client_id', label: 'Client ID', type: 'string', required: true, secret: false },
    { key: 'client_secret', label: 'Client Secret', type: 'string', required: true, secret: true },
  ],
}

describe('AdapterForm', () => {
  beforeEach(() => vi.mocked(testAdapter).mockReset())
  afterEach(() => vi.clearAllMocks())

  it('renders one input per schema field', () => {
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    expect(screen.getByLabelText('Client ID')).toBeInTheDocument()
    expect(screen.getByLabelText('Client Secret')).toBeInTheDocument()
  })

  it('renders secret fields as password inputs', () => {
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    const secret = screen.getByLabelText('Client Secret') as HTMLInputElement
    expect(secret.type).toBe('password')
  })

  it('shows "set" placeholder for an already-set secret and keeps the value hidden', () => {
    render(
      <AdapterForm name="spotify" schema={schema} initial={{ client_id: 'abc', client_secret__isSet: true }} onSubmit={vi.fn()} />,
    )
    const secret = screen.getByLabelText('Client Secret') as HTMLInputElement
    expect(secret.value).toBe('') // never the real value
    expect(secret.placeholder).toMatch(/leave blank/i)
    const id = screen.getByLabelText('Client ID') as HTMLInputElement
    expect(id.value).toBe('abc')
  })

  it('Test Connection calls testAdapter and shows the result', async () => {
    vi.mocked(testAdapter).mockResolvedValue({ ok: false, error: 'connection refused' })
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Client ID'), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText('Client Secret'), { target: { value: 'shh' } })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))
    await waitFor(() => expect(testAdapter).toHaveBeenCalledWith('spotify', { client_id: 'x', client_secret: 'shh' }))
    expect(await screen.findByText(/connection refused/i)).toBeInTheDocument()
  })

  it('submits the collected config', async () => {
    const onSubmit = vi.fn()
    render(<AdapterForm name="spotify" schema={schema} onSubmit={onSubmit} />)
    fireEvent.change(screen.getByLabelText('Client ID'), { target: { value: 'cid' } })
    fireEvent.change(screen.getByLabelText('Client Secret'), { target: { value: 'csec' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ client_id: 'cid', client_secret: 'csec' }))
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- AdapterForm`
Expected: FAIL — `Cannot find module './AdapterForm'`.

- [ ] **Step 3: Write AdapterForm.tsx**

Create `web/src/components/AdapterForm.tsx`:
```tsx
import { useMemo, useState } from 'react'
import type { ConfigField, ConfigSchema } from '../lib/adaptersApi'
import { testAdapter } from '../lib/adaptersApi'

export interface AdapterFormProps {
  name: string
  schema: ConfigSchema
  initial?: Record<string, unknown>
  submitLabel?: string
  onSubmit: (config: Record<string, unknown>) => void | Promise<void>
}

type FieldValue = string | boolean

// initialValue derives the form value for a field from the (redacted) initial config.
// Secret fields always start blank (the real value is never sent to the browser).
function initialValue(f: ConfigField, initial?: Record<string, unknown>): FieldValue {
  if (f.type === 'bool') return Boolean(initial?.[f.key])
  if (f.secret) return ''
  const v = initial?.[f.key]
  return v == null ? '' : String(v)
}

// collect builds the config object to submit/test from the current field values.
// number fields are coerced; bool stays boolean; everything else is a string.
function collect(schema: ConfigSchema, values: Record<string, FieldValue>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of schema.fields) {
    const v = values[f.key]
    if (f.type === 'number') {
      out[f.key] = v === '' ? '' : Number(v)
    } else {
      out[f.key] = v
    }
  }
  return out
}

export function AdapterForm({ name, schema, initial, submitLabel = 'Save', onSubmit }: AdapterFormProps) {
  const [values, setValues] = useState<Record<string, FieldValue>>(() => {
    const v: Record<string, FieldValue> = {}
    for (const f of schema.fields) v[f.key] = initialValue(f, initial)
    return v
  })
  const [testState, setTestState] = useState<{ status: 'idle' | 'testing' | 'ok' | 'error'; msg?: string }>({ status: 'idle' })
  const [submitting, setSubmitting] = useState(false)

  const secretIsSet = useMemo(() => {
    const m: Record<string, boolean> = {}
    for (const f of schema.fields) if (f.secret) m[f.key] = Boolean(initial?.[`${f.key}__isSet`])
    return m
  }, [schema, initial])

  function set(key: string, v: FieldValue) {
    setValues((prev) => ({ ...prev, [key]: v }))
  }

  async function runTest() {
    setTestState({ status: 'testing' })
    try {
      const res = await testAdapter(name, collect(schema, values))
      setTestState(res.ok ? { status: 'ok' } : { status: 'error', msg: res.error || 'Connection failed' })
    } catch (e) {
      setTestState({ status: 'error', msg: e instanceof Error ? e.message : 'Connection failed' })
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    try {
      await onSubmit(collect(schema, values))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={submit} className="space-y-3">
      {schema.fields.map((f) => (
        <div key={f.key} className="space-y-1">
          <label htmlFor={`field-${f.key}`} className="block text-sm text-neutral-300">
            {f.label}
            {f.required && <span className="ml-1 text-accent">*</span>}
          </label>
          {f.type === 'bool' ? (
            <input
              id={`field-${f.key}`}
              type="checkbox"
              checked={Boolean(values[f.key])}
              onChange={(e) => set(f.key, e.target.checked)}
            />
          ) : (
            <input
              id={`field-${f.key}`}
              type={f.secret ? 'password' : f.type === 'number' ? 'number' : 'text'}
              value={String(values[f.key] ?? '')}
              onChange={(e) => set(f.key, e.target.value)}
              placeholder={f.secret && secretIsSet[f.key] ? 'Leave blank to keep current value' : ''}
              className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
            />
          )}
        </div>
      ))}

      <div className="flex items-center gap-3 pt-1">
        <button type="submit" disabled={submitting} className="rounded bg-accent px-4 py-2 font-medium text-white disabled:opacity-50">
          {submitLabel}
        </button>
        <button type="button" onClick={runTest} disabled={testState.status === 'testing'} className="rounded border border-neutral-700 px-4 py-2 text-neutral-200 hover:bg-neutral-800">
          {testState.status === 'testing' ? 'Testing…' : 'Test Connection'}
        </button>
        {testState.status === 'ok' && <span className="text-sm text-green-400">✓ Connection OK</span>}
        {testState.status === 'error' && <span className="text-sm text-accent">✗ {testState.msg}</span>}
      </div>
    </form>
  )
}
```

- [ ] **Step 4: Run the tests + typecheck**

Run: `cd web && npm run test -- AdapterForm`
Expected: PASS (all five).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/AdapterForm.tsx web/src/components/AdapterForm.test.tsx
git commit -m "feat(web): ConfigSchema-driven AdapterForm with secret masking and Test Connection"
```

---

## Task 10: Settings page rewrite — manage adapters + accent/dynamic-bg + restart banner

**Files:**
- Rewrite: `web/src/routes/Settings.tsx`
- Test: `web/src/routes/Settings.test.tsx` (NEW)

**Interfaces:**
- Consumes: `useAdapters`, `useAvailableAdapters`, `usePendingRestart`, `createAdapter`, `updateAdapter`, `deleteAdapter` (adaptersApi); `useSettings`, `putSettings`, `applyAccent` (settingsApi); `AdapterForm`; TanStack `useQueryClient`.
- Behavior:
  - Three sections by type: **Library**, **Search**, **Downloaders** (filter `useAdapters()` by `type === 'library'|'search'|'downloader'`).
  - Each section lists configured instances (name + enabled toggle + priority up/down + Edit + Remove). "Add" picks from `useAvailableAdapters()` filtered to that type, then renders an `AdapterForm` with that adapter's `configSchema`.
  - Editing renders `AdapterForm` pre-filled with the instance's redacted `config`.
  - Enable toggle / reorder call `updateAdapter` (carrying the redacted config back — secrets preserved by the backend since redacted secret values are blank/omitted).
  - **Appearance** section: accent color `<input type="color">` (writes `accent_color` via `putSettings`, calls `applyAccent` live) + dynamic_background checkbox.
  - **Pending-restart banner** at the top when `usePendingRestart().data.pendingRestart` is true.
  - On any adapter mutation, invalidate `['adapters','list']` and `['config','pending-restart']`.

> **Reorder/toggle secret-safety:** when calling `updateAdapter` from a toggle/reorder (not the full form), pass the instance's `config` as-is — its secret fields are already redacted (value absent), so `mergeSecrets` on the backend preserves the stored secret. Strip the `__isSet` sidecars before sending (the backend strips them anyway, but keep the payload clean).

- [ ] **Step 1: Write the failing test**

Create `web/src/routes/Settings.test.tsx`:
```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Settings from './Settings'

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: vi.fn(),
  useAvailableAdapters: vi.fn(),
  usePendingRestart: vi.fn(),
  createAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  updateAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  deleteAdapter: vi.fn(() => Promise.resolve({ ok: true, pendingRestart: true })),
  // testAdapter must be included: AdapterForm (imported by Settings) uses it, and a
  // full vi.mock factory must export EVERY symbol the module's consumers import or
  // Vitest throws "No 'testAdapter' export" at import time.
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))
vi.mock('../lib/settingsApi', () => ({
  useSettings: vi.fn(() => ({ data: { accentColor: '#F0354B', dynamicBackground: true } })),
  putSettings: vi.fn(() => Promise.resolve({ accentColor: '#F0354B', dynamicBackground: true })),
  applyAccent: vi.fn(),
}))
import { useAdapters, useAvailableAdapters, usePendingRestart, deleteAdapter } from '../lib/adaptersApi'

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('Settings', () => {
  beforeEach(() => {
    vi.mocked(useAvailableAdapters).mockReturnValue({ data: [{ type: 'search', name: 'spotify', configSchema: { fields: [] }, capabilities: [] }] } as ReturnType<typeof useAvailableAdapters>)
    vi.mocked(usePendingRestart).mockReturnValue({ data: { pendingRestart: false } } as ReturnType<typeof usePendingRestart>)
    vi.mocked(useAdapters).mockReturnValue({
      data: [{ id: 'a1', type: 'search', name: 'spotify', enabled: true, priority: 0, config: { client_id: 'x', client_secret__isSet: true } }],
    } as ReturnType<typeof useAdapters>)
  })
  afterEach(() => vi.clearAllMocks())

  it('lists configured instances', () => {
    wrap(<Settings />)
    expect(screen.getByText(/spotify/i)).toBeInTheDocument()
  })

  it('shows the restart banner when pending', () => {
    vi.mocked(usePendingRestart).mockReturnValue({ data: { pendingRestart: true } } as ReturnType<typeof usePendingRestart>)
    wrap(<Settings />)
    expect(screen.getByText(/restart reverb to apply/i)).toBeInTheDocument()
  })

  it('removes an instance', async () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /remove a1/i }))
    await waitFor(() => expect(deleteAdapter).toHaveBeenCalledWith('a1'))
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- Settings`
Expected: FAIL (current `Settings.tsx` is a placeholder `<h1>` — assertions fail).

- [ ] **Step 3: Rewrite Settings.tsx**

Replace `web/src/routes/Settings.tsx` with:
```tsx
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useAdapters,
  useAvailableAdapters,
  usePendingRestart,
  createAdapter,
  updateAdapter,
  deleteAdapter,
  type AdapterInstance,
  type AvailableAdapter,
} from '../lib/adaptersApi'
import { useSettings, putSettings, applyAccent } from '../lib/settingsApi'
import { AdapterForm } from '../components/AdapterForm'

const SECTIONS: { type: string; title: string }[] = [
  { type: 'library', title: 'Library' },
  { type: 'search', title: 'Search' },
  { type: 'downloader', title: 'Downloaders' },
]

// stripIsSet removes the "<key>__isSet" sidecars before re-sending a redacted config.
function stripIsSet(config: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const k of Object.keys(config)) {
    if (k.endsWith('__isSet')) continue
    out[k] = config[k]
  }
  return out
}

export default function Settings() {
  const qc = useQueryClient()
  const adapters = useAdapters()
  const available = useAvailableAdapters()
  const pending = usePendingRestart()
  const settings = useSettings()

  const [editing, setEditing] = useState<{ section: string; instance?: AdapterInstance; add?: AvailableAdapter } | null>(null)

  function refresh() {
    void qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
    void qc.invalidateQueries({ queryKey: ['config', 'pending-restart'] })
  }

  async function onRemove(id: string) {
    await deleteAdapter(id)
    refresh()
  }
  async function onToggle(inst: AdapterInstance) {
    await updateAdapter(inst.id, { name: inst.name, enabled: !inst.enabled, priority: inst.priority, config: stripIsSet(inst.config) })
    refresh()
  }
  async function onReorder(inst: AdapterInstance, delta: number) {
    await updateAdapter(inst.id, { name: inst.name, enabled: inst.enabled, priority: inst.priority + delta, config: stripIsSet(inst.config) })
    refresh()
  }

  const list = adapters.data ?? []
  const avail = available.data ?? []

  return (
    <div className="max-w-2xl space-y-8">
      <h1 className="text-2xl font-bold">Settings</h1>

      {pending.data?.pendingRestart && (
        <div className="rounded border border-accent/50 bg-accent/10 px-4 py-3 text-sm text-accent">
          Restart Reverb to apply your configuration changes.
        </div>
      )}

      {SECTIONS.map((sec) => {
        const items = list.filter((a) => a.type === sec.type).sort((a, b) => a.priority - b.priority)
        const choices = avail.filter((a) => a.type === sec.type)
        return (
          <section key={sec.type} className="space-y-2">
            <h2 className="text-lg font-semibold">{sec.title}</h2>
            <ul className="space-y-1">
              {items.length === 0 && <li className="text-sm text-neutral-500">None configured.</li>}
              {items.map((inst) => (
                <li key={inst.id} className="flex items-center gap-2 rounded bg-neutral-900 px-3 py-2">
                  <span className="flex-1">{inst.name}</span>
                  <button type="button" aria-label={`Toggle ${inst.id}`} onClick={() => void onToggle(inst)} className="text-sm text-neutral-300">
                    {inst.enabled ? 'Enabled' : 'Disabled'}
                  </button>
                  <button type="button" aria-label={`Move up ${inst.id}`} onClick={() => void onReorder(inst, -1)} className="text-neutral-400">↑</button>
                  <button type="button" aria-label={`Move down ${inst.id}`} onClick={() => void onReorder(inst, 1)} className="text-neutral-400">↓</button>
                  <button type="button" aria-label={`Edit ${inst.id}`} onClick={() => setEditing({ section: sec.type, instance: inst })} className="text-sm text-neutral-300">Edit</button>
                  <button type="button" aria-label={`Remove ${inst.id}`} onClick={() => void onRemove(inst.id)} className="text-sm text-accent">Remove</button>
                </li>
              ))}
            </ul>
            <div className="flex flex-wrap gap-2">
              {choices.map((c) => (
                <button key={c.name} type="button" onClick={() => setEditing({ section: sec.type, add: c })} className="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800">
                  + Add {c.name}
                </button>
              ))}
            </div>
          </section>
        )
      })}

      {editing && (() => {
        const schema = editing.add?.configSchema ?? avail.find((a) => a.name === editing.instance?.name)?.configSchema ?? { fields: [] }
        const name = editing.add?.name ?? editing.instance?.name ?? ''
        const initial = editing.instance?.config
        return (
          <div className="rounded border border-neutral-700 p-4">
            <h3 className="mb-3 font-semibold">{editing.add ? `Add ${name}` : `Edit ${name}`}</h3>
            <AdapterForm
              name={name}
              schema={schema}
              initial={initial}
              submitLabel={editing.add ? 'Add' : 'Save'}
              onSubmit={async (config) => {
                if (editing.add) {
                  await createAdapter({ type: editing.section, name, enabled: true, priority: 0, config })
                } else if (editing.instance) {
                  await updateAdapter(editing.instance.id, {
                    name: editing.instance.name,
                    enabled: editing.instance.enabled,
                    priority: editing.instance.priority,
                    config,
                  })
                }
                setEditing(null)
                refresh()
              }}
            />
            <button type="button" onClick={() => setEditing(null)} className="mt-2 text-sm text-neutral-400">Cancel</button>
          </div>
        )
      })()}

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Appearance</h2>
        <div className="flex items-center gap-3">
          <label htmlFor="accent" className="text-sm text-neutral-300">Accent color</label>
          <input
            id="accent"
            type="color"
            value={settings.data?.accentColor ?? '#F0354B'}
            onChange={(e) => {
              applyAccent(e.target.value)
              void putSettings({ accentColor: e.target.value }).then(() => qc.invalidateQueries({ queryKey: ['settings'] }))
            }}
          />
        </div>
        <div className="flex items-center gap-2">
          <input
            id="dynbg"
            type="checkbox"
            checked={settings.data?.dynamicBackground ?? true}
            onChange={(e) => void putSettings({ dynamicBackground: e.target.checked }).then(() => qc.invalidateQueries({ queryKey: ['settings'] }))}
          />
          <label htmlFor="dynbg" className="text-sm text-neutral-300">Dynamic album background</label>
        </div>
      </section>
    </div>
  )
}
```

- [ ] **Step 4: Run the tests + typecheck**

Run: `cd web && npm run test -- Settings`
Expected: PASS (lists, banner, remove).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/Settings.tsx web/src/routes/Settings.test.tsx
git commit -m "feat(web): Settings page — manage adapters, accent picker, dynamic-bg, restart banner"
```

---

## Task 11: First-run wizard — multi-step Setup reusing AdapterForm

**Files:**
- Rewrite: `web/src/routes/Setup.tsx`
- Modify: `web/src/routes/Setup.test.tsx`

**Interfaces:**
- Consumes: `api.post('/setup/admin')` (existing), `useAvailableAdapters`, `createAdapter` (adaptersApi), `AdapterForm`.
- Behavior — steps:
  1. **Admin password** (existing behavior). On success, advance to step 2 (do NOT reload yet).
  2. **Library** — pick an available `library` adapter (Subsonic), render `AdapterForm`; on Save → `createAdapter({type:'library',...})` then advance. "Skip" advances without adding.
  3. **Search** — same for `search` (Spotify). Skippable.
  4. **Downloader** — same for `downloader` (spotDL). Skippable.
  5. **Finish** — `window.location.reload()` (so the app re-bootstraps and the guard routes to `/search`; the freshly-added adapters load on the NEXT process start under restart-to-apply — for a brand-new install the process is already running with zero adapters, so the wizard's banner-equivalent message reads: "Setup complete. Restart Reverb so your library, search, and downloader become active.").

> **Restart honesty in the wizard:** since adapters apply on restart (option A), the finish screen tells the user to restart Reverb for the newly added adapters to become active. This is the same honest UX as the Settings banner. The admin password takes effect immediately (auth is checked live), so login works without restart; only the adapters need the restart.

- [ ] **Step 1: Write/extend the failing test**

Replace `web/src/routes/Setup.test.tsx` with:
```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Setup from './Setup'

vi.mock('../lib/api', () => ({ api: { post: vi.fn(() => Promise.resolve({ ok: true })) } }))
vi.mock('../lib/adaptersApi', () => ({
  useAvailableAdapters: vi.fn(() => ({ data: [{ type: 'library', name: 'subsonic', configSchema: { fields: [] }, capabilities: [] }] })),
  createAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  // testAdapter + SECRET_SENTINEL must be included: Setup imports AdapterForm which
  // imports { testAdapter, SECRET_SENTINEL } from adaptersApi. A full vi.mock factory
  // must export every symbol or Vitest throws "No 'testAdapter' export" at import time.
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))
import { api } from '../lib/api'

function renderSetup() {
  return render(
    <MemoryRouter>
      <Setup />
    </MemoryRouter>,
  )
}

describe('Setup wizard', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.clearAllMocks())

  it('step 1 prompts for an admin password', () => {
    renderSetup()
    expect(screen.getByText('Welcome to Reverb')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Choose a password')).toBeInTheDocument()
  })

  it('advances to the Library step after setting a password', async () => {
    renderSetup()
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/setup/admin', { password: 'hunter2' }))
    expect(await screen.findByText(/add a library/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- Setup`
Expected: FAIL — current Setup has no step-2 "add a library" text.

- [ ] **Step 3: Rewrite Setup.tsx**

Replace `web/src/routes/Setup.tsx` with:
```tsx
import { useState } from 'react'
import { api } from '../lib/api'
import { useAvailableAdapters, createAdapter, type AvailableAdapter } from '../lib/adaptersApi'
import { AdapterForm } from '../components/AdapterForm'

type Step = 'password' | 'library' | 'search' | 'downloader' | 'done'

const NEXT: Record<Step, Step> = {
  password: 'library',
  library: 'search',
  search: 'downloader',
  downloader: 'done',
  done: 'done',
}

const STEP_COPY: Record<Exclude<Step, 'password' | 'done'>, { type: string; title: string }> = {
  library: { type: 'library', title: 'Add a Library' },
  search: { type: 'search', title: 'Add a Search source' },
  downloader: { type: 'downloader', title: 'Add a Downloader' },
}

export default function Setup() {
  const [step, setStep] = useState<Step>('password')
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const available = useAvailableAdapters()
  const [chosen, setChosen] = useState<AvailableAdapter | null>(null)

  async function submitPassword(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    try {
      await api.post('/setup/admin', { password: pw })
      setStep('library')
    } catch {
      setErr('Could not complete setup. Please try again.')
    }
  }

  function advance() {
    setChosen(null)
    setStep((s) => NEXT[s])
  }

  if (step === 'password') {
    return (
      <form onSubmit={submitPassword} className="max-w-sm mx-auto mt-24 space-y-4">
        <h1 className="text-2xl font-bold">Welcome to Reverb</h1>
        <p className="text-neutral-400 text-sm">Set an admin password to get started.</p>
        <input
          type="password"
          value={pw}
          onChange={(e) => setPw(e.target.value)}
          className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
          placeholder="Choose a password"
        />
        {err && <p className="text-accent text-sm">{err}</p>}
        <button type="submit" className="w-full rounded bg-accent py-2 font-medium text-white">Continue</button>
      </form>
    )
  }

  if (step === 'done') {
    return (
      <div className="max-w-md mx-auto mt-24 space-y-4 text-center">
        <h1 className="text-2xl font-bold">You're all set</h1>
        <p className="text-neutral-400 text-sm">
          Setup complete. Restart Reverb so your library, search, and downloader become active, then log in.
        </p>
        <button type="button" onClick={() => window.location.reload()} className="rounded bg-accent px-6 py-2 font-medium text-white">
          Go to Reverb
        </button>
      </div>
    )
  }

  const copy = STEP_COPY[step]
  const choices = (available.data ?? []).filter((a) => a.type === copy.type)

  return (
    <div className="max-w-md mx-auto mt-20 space-y-4">
      <h1 className="text-2xl font-bold">{copy.title}</h1>
      {!chosen && (
        <div className="space-y-3">
          <div className="flex flex-wrap gap-2">
            {choices.map((c) => (
              <button key={c.name} type="button" onClick={() => setChosen(c)} className="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800">
                {c.name}
              </button>
            ))}
            {choices.length === 0 && <p className="text-sm text-neutral-500">No adapters available for this step.</p>}
          </div>
          <button type="button" onClick={advance} className="text-sm text-neutral-400">Skip this step</button>
        </div>
      )}
      {chosen && (
        <div className="rounded border border-neutral-700 p-4">
          <h3 className="mb-3 font-semibold">{chosen.name}</h3>
          <AdapterForm
            name={chosen.name}
            schema={chosen.configSchema}
            submitLabel="Add"
            onSubmit={async (config) => {
              await createAdapter({ type: copy.type, name: chosen.name, enabled: true, priority: 0, config })
              advance()
            }}
          />
          <button type="button" onClick={() => setChosen(null)} className="mt-2 text-sm text-neutral-400">Back</button>
        </div>
      )}
    </div>
  )
}
```

> **Guard note:** `web/src/App.tsx` already routes a fresh install to `<Setup />` while `setup_required` (i.e. no admin password). After step 1 the wizard advances IN-MEMORY (the App guard is not re-evaluated until reload), so steps 2–4 render even though `setup_required` is now false server-side. The finish "Go to Reverb" reload re-runs the guard, which now sees `setup_required:false` and routes to `<Login />`. No App.tsx change is required for the wizard, but note that `useAvailableAdapters` must work before login: it is behind `requireAuth`, and the wizard's session cookie was set by `/setup/admin` (which calls `issueSession`). So the user IS authed during steps 2–4. Good — no guard change needed.

- [ ] **Step 4: Run the tests + typecheck**

Run: `cd web && npm run test -- Setup`
Expected: PASS (step-1 prompt, advance to Library).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/Setup.tsx web/src/routes/Setup.test.tsx
git commit -m "feat(web): first-run wizard — admin password then add Library/Search/Downloader via AdapterForm"
```

---

## Task 12: Apply accent color live at app bootstrap

**Files:**
- Modify: `web/src/main.tsx`
- Test: covered by `settingsApi.test.ts` (applyAccent) + a small main bootstrap assertion is optional; this task is wiring.

**Interfaces:**
- Consumes: `getSettings`, `applyAccent` (settingsApi).
- Behavior: before/at app mount, best-effort fetch `/settings` and call `applyAccent(accentColor)` so the saved accent themes the app immediately on load. The default `--color-accent` in `index.css` (`240 53 75`) is the fallback if the fetch fails or the user is unauthenticated (settings is behind auth — a logged-out user keeps the default red, which is correct).

- [ ] **Step 1: Read the current main.tsx**

Read `web/src/main.tsx` to see the mount code (it renders `<App />` into `#root`). It currently has no settings bootstrap.

- [ ] **Step 2: Add the accent bootstrap**

Edit `web/src/main.tsx` — after the imports and before/around `createRoot(...).render(...)`, add a best-effort accent bootstrap. Example shape (adapt to the existing render call; keep the existing render):
```tsx
import { getSettings, applyAccent } from './lib/settingsApi'

// Best-effort: theme the app with the saved accent before the user notices.
// Fails silently when logged out (settings is auth-gated) — the CSS default red wins.
void getSettings()
  .then((s) => applyAccent(s.accentColor))
  .catch(() => {})
```
Place this import alongside the others and the `void getSettings()...` block immediately before the `createRoot` render call.

- [ ] **Step 3: Verify**

Run: `cd web && npm run build`
Expected: clean typecheck + build.
Run: `cd web && npm run test`
Expected: the full Vitest suite passes (no regressions; `settingsApi.test.ts` covers `applyAccent`).

- [ ] **Step 4: Commit**

```bash
git add web/src/main.tsx
git commit -m "feat(web): apply saved accent color live at app bootstrap"
```

---

## Task 13: Backend integration smoke — full adapter lifecycle through the API

**Files:**
- Test: `internal/api/adapters_smoke_test.go` (NEW)

**Interfaces:**
- Consumes: the assembled `Server` (real `store.Store`, real `auth.Service`, a registry with the controllable `fakeAdapter`).
- A single end-to-end test that walks: create → list (redacted) → test (ok) → update (preserve secret) → toggle via update → delete → list empty → pending-restart true.

- [ ] **Step 1: Write the smoke test**

Create `internal/api/adapters_smoke_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdapterLifecycleSmoke(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty, testErr: nil})

	// 1. create
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"sekret"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data adapterInstanceDTO `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Data.ID
	if id == "" {
		t.Fatal("no id returned")
	}

	// 2. list redacts the secret
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	var list []adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Config["token__isSet"] != true {
		t.Fatalf("list redaction failed: %+v", list)
	}
	if _, leaked := list[0].Config["token"]; leaked {
		t.Fatal("secret leaked in list")
	}

	// 3. test ok
	rec = do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"fake","config":{"url":"http://x","token":"sekret"}}`)
	var test struct{ OK bool `json:"ok"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &test)
	if !test.OK {
		t.Fatalf("test should be ok, got %s", rec.Body.String())
	}

	// 4. update with blank secret preserves it
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/adapters/"+id,
		`{"name":"fake","enabled":true,"priority":2,"config":{"url":"http://y","token":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d", rec.Code)
	}
	raw, _ := getStoredInstance(t, srv, id)
	var stored map[string]any
	_ = json.Unmarshal([]byte(raw), &stored)
	if stored["token"] != "sekret" || stored["url"] != "http://y" {
		t.Fatalf("preserve+update failed: %+v", stored)
	}

	// 5. delete
	rec = do(t, srv, cookie, http.MethodDelete, "/api/v1/adapters/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	list = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("want empty after delete, got %d", len(list))
	}

	// 6. config is dirty (restart-to-apply)
	if !dirty.Dirty() {
		t.Fatal("config should be dirty after mutations")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/api/ -run TestAdapterLifecycleSmoke -v`
Expected: PASS.

- [ ] **Step 3: Run the FULL backend + frontend suites**

Run:
```bash
go test ./cmd/... ./internal/...
```
Expected: PASS (all packages).
Run:
```bash
cd web && npm run test && npm run build
```
Expected: Vitest green; typecheck + build clean.

- [ ] **Step 4: Commit**

```bash
git add internal/api/adapters_smoke_test.go
git commit -m "test(api): full adapter lifecycle smoke — create/list/test/update/delete/dirty"
```

---

## Definition of Done (M4a)

- [ ] **Store:** `adapter_instances` get-by-id, update, set-enabled, set-priority queries exist and are sqlc-generated + committed; CRUD round-trip test passes. No new migration (existing table reused).
- [ ] **Secret handling:** `redactConfig` removes Secret:true values and emits `<key>__isSet`; `mergeSecrets` preserves stored secrets on blank/sentinel, overwrites on a real value, strips `__isSet` sidecars. Unit-tested. NO endpoint ever returns a stored secret value.
- [ ] **Adapters API:** `GET /adapters` (redacted), `POST /adapters`, `PUT /adapters/:id` (secret-preserving), `DELETE /adapters/:id`, `POST /adapters/test` (non-persisted instantiate → Init+env overlay → TestConnection, timeout-bounded, ok/error never 5xx for a connection failure). `GET /adapters/available` includes Library, Search, and Downloader registries. All behind `requireAuth`. Tested.
- [ ] **Settings API:** `GET /settings` (defaults accent `#F0354B`, dynamic_background true) + `PUT /settings`. Behind `requireAuth`. Tested.
- [ ] **Restart-to-apply:** an `*atomic.Bool` dirty flag flips on any adapter create/update/delete; `GET /config/pending-restart` + a `pendingRestart` field on mutation responses expose it; nil-safe. Tested.
- [ ] **Composition root:** `api.Deps` gains `Adapters` (=`st.Q()`), `Lib` (library registry), `ConfigDirty`; `go build ./cmd/... ./internal/...` clean; all `NewServer(Deps{})` call sites compile.
- [ ] **Frontend clients:** `api.put`/`api.del`; `adaptersApi.ts` (+ hooks) and `settingsApi.ts` (+ `hexToRgbChannels`/`applyAccent` + hooks). Vitest stubbing fetch passes.
- [ ] **AdapterForm:** renders from a ConfigSchema (text/password[secret]/number/bool), secret fields are password inputs showing set/unset (never the value, blank preserves), Test Connection button shows ok/error. Tested.
- [ ] **Settings page:** sections per type (Library/Search/Downloaders), list/add/edit/remove/enable-toggle/reorder, accent-color picker (writes setting + applies `--color-accent` live), dynamic_background toggle, pending-restart banner. Tested.
- [ ] **Wizard:** Setup is a multi-step flow — admin password → Library → Search → Downloader → finish — reusing AdapterForm; honest restart message; the App guard routes a fresh install through it. Tested.
- [ ] **Accent live:** accent color applied to `--color-accent` at app bootstrap and on change; default red `#F0354B`.
- [ ] **The whole point:** a brand-new user can `docker compose up`, set a password, add Navidrome + Spotify + spotDL through forms with Test Connection, restart, and use Reverb — NO sqlite hand-editing.
- [ ] `go test ./cmd/... ./internal/...` green; `cd web && npm run test && npm run build` green. No real network in tests.

## Self-Review

- **Coverage vs M4a scope:** store update queries (T1) ✓; secret redaction/preserve (T2) ✓; adapters CRUD API (T3–T4) ✓; `/adapters/test` non-persisted (T5) ✓; settings API (T6) ✓; pending-restart flag (T3 + T7) ✓; wizard backend reuses adapters API (no new backend needed — admin password already exists; T11 frontend) ✓; frontend adaptersApi/settingsApi + hooks (T8) ✓; AdapterForm + Test Connection (T9) ✓; Settings rewrite (T10) ✓; wizard (T11) ✓; accent live (T8 helper + T10 picker + T12 bootstrap) ✓; smoke (T13) ✓.
- **Apply-config decision is concrete:** restart-to-apply (option A), stated in Architecture + Global Constraints, implemented via `*atomic.Bool` `atomicDirty` (T7), flipped in every adapter mutation handler (T4 `markDirty`), exposed via `GET /config/pending-restart` (T3) + `pendingRestart` on mutation responses (T4 `writeJSONPending`), surfaced as a banner (T10) and an honest wizard finish (T11). Hot-reload explicitly deferred with justification (running workers/in-flight execs/open SSE streams).
- **Secret-redaction decision is concrete + generic:** consults each adapter's `ConfigSchema` `Secret:true` fields only (no per-adapter hardcoding) — `secretKeys`/`redactConfig`/`mergeSecrets` (T2). Verified: list never returns a secret value (T4 `TestCreateThenListRedactsSecret`, T13 smoke step 2); blank/sentinel preserves (T2 + T4 `TestUpdatePreservesSecretWhenBlank` + T13 step 4); real value overwrites (T4 `TestUpdateNewSecretOverwrites`); `__isSet` sidecars never persisted (T2) and stripped before Init in `/test` (T5).
- **Type consistency (Go):** `Deps.Adapters AdapterStore` satisfied directly by `*db.Queries` (all 7 methods exist: List/Get/Create/Update/Delete adapter + GetSetting/UpsertSetting) — compile-checked at the `main.go` literal (T7). New sqlc param structs named via `@`-params (T1). `boolToInt`/`int64` conversions explicit. `chi.URLParam(r,"id")` matches the `{id}` route patterns. `writeJSONPending` wraps create/update as `{data,pendingRestart}`; LIST and GET are unwrapped — the plan flags the test-parse adjustment inline (T4 DTO shape note).
- **Type consistency (TS):** `AdapterInstance.config` is `Record<string, unknown>` carrying `<key>__isSet` sidecars for secrets; AdapterForm reads `initial["<key>__isSet"]` and never the value; create/update return `Wrapped<T> = {data,pendingRestart}` matching the backend; delete returns `{ok,pendingRestart}`; `testAdapter` returns `{ok,error?}`. `hexToRgbChannels("#F0354B") === "240 53 75"` matches the `index.css` default token.
- **Nil-safety / call-site compilation:** every new `Deps` field is an interface or `*registry.Registry`; nil checks in `handlePendingRestart`, `handleListAdapters`, `handleCreateAdapter`, settings handlers; `markDirty`/`dirtyNow` guard `ConfigDirty == nil`. Existing `NewServer(Deps{...})` sites (downloads_test, search_test, etc.) compile because new fields default to nil. The api test helper sets `Adapters: st.Q()`, `Lib/Search/Downloader` registries, `ConfigDirty`.
- **Auth:** all new routes are inside the `requireAuth` group (T3). Tests assert 401 without a cookie (T4, T6). The wizard is authed after step 1 (`/setup/admin` issues a session) so `useAvailableAdapters`/`createAdapter` work during steps 2–4 (T11 guard note).
- **Risks / open questions:**
  1. **Inter-task compile order (T3 ↔ T4):** the routes in T3 reference handlers defined in T4–T6; the plan ships 501 stubs in T3 so the package always compiles, replaced in T4–T6. `config_test.go` (T3) depends on `adapterTestServer` (T4); the plan notes Tasks 3+4 land together. An implementer doing strict one-task-per-PR should do T4 Step 1 (the helper) before T3 Step 2 — flagged inline.
  2. **`/test` env overlay duplication:** `overlayEnvSecrets` (T5) duplicates the per-adapter env-var knowledge in `*_wiring.go`. Acceptable for M4a (small, explicit); a future refactor could centralize the env-secret map. Low risk.
  3. **Reorder uses absolute priority deltas** (T10 `onReorder` ±1) rather than a swap; two same-type instances could end up with equal priorities. For MVP (typically 1 library, 1 search, 1 downloader) this is harmless; a swap-based reorder is a fast-follow. Low risk.
  4. **`config_json` shape from older milestones:** M1–M3 hand-seeded rows store plain config (e.g. `{"client_secret":"..."}`). `redactConfig` handles these correctly (it redacts by schema regardless of how the row was created), so the new UI safely manages legacy rows. Verified by design.
  5. **Live accent for logged-out users:** `/settings` is auth-gated, so the bootstrap fetch (T12) fails silently when logged out and the CSS default red applies — correct and intended.
