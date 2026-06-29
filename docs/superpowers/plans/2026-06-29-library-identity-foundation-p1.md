# Library Identity Foundation — P1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the backend-independent canonical identity layer (P1 of the library-identity-foundation spec) so listening history, playlists, covers, and playback survive a backend swap or re-scan — and fix the two consumers that silently give wrong answers on a swap today.

**Architecture:** A new `catalog` package mints a stable surrogate id per library entity, deduped by durable aliases (ISRC / external id / a matcher-grade `norm` fingerprint). A new `resolver` package maps a canonical id → current backend addressing, cache-first via a new `backend_binding` table, invalidated by a per-identity epoch (not the global `library_version`), re-deriving via the existing matcher and reading the live adapter through a provider so it survives hot-reloads. The cover/stream handlers prefix-discriminate canonical vs raw backend ids and resolve→validate→fallback in one place. Two consumers (synced-playlist library tracks, download-job links) move to resolve-at-read.

**Tech Stack:** Go (chi, modernc cgo-free sqlite, goose migrations, sqlc), React 19 / TS (Vite, Zustand, Vitest, Playwright).

## Global Constraints

- **Generated code is generated:** never hand-edit `internal/store/db/*.sql.go` or `models.go`. Edit `internal/store/queries/*.sql` + run `make generate`. Migrations are goose files; **latest applied is `0018_notifications.sql`** — add `0019_catalog.sql`, never edit applied ones.
- **Migration `0019` is PURELY ADDITIVE:** three `CREATE TABLE`s only. No `ALTER`, no backfill, no `INSERT`. `Down` drops the three tables (children first). (Spec §7.1.)
- **No `canonical_id` columns on consumer tables in P1** — derive at reference time from the durable columns they already carry. (Spec §7.2.)
- **Design tokens only** in FE — no raw hex, no `text-black`/`text-white`; accent surfaces use `text-on-accent`. Match existing component density/idioms.
- **The gate must be green before merge:** repo root `go test ./... && go build ./... && go vet ./...`; from `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.
- **Commit footer:** end every commit message with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Branch:** all work on `feat/library-identity-foundation` (already created; the spec commit is its first commit). Never commit to `main`.
- **sqlite id type:** all ids are `TEXT`; timestamps are `INTEGER` unix-seconds (mirror `0018_notifications.sql`).

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/migrations/0019_catalog.sql` | The 3 new tables (additive). |
| `internal/store/queries/catalog.sql` | sqlc queries for entity/alias/binding (+ merge repoint). |
| `internal/store/db/catalog.sql.go`, `models.go` | **Generated** by `make generate`. |
| `internal/matching/fingerprint.go` (+ `_test.go`) | The `norm` composite fingerprint (reuses `Normalize`/dedup). |
| `internal/catalog/catalog.go` | `Identity`, `Entity`, narrow `Querier`, `Service` constructor. |
| `internal/catalog/canonical.go` (+ `_test.go`) | `CanonicalFor` (alias lookup + lazy mint). |
| `internal/catalog/merge.go` (+ `_test.go`) | `merge(loser, winner)` + collision/corroboration. |
| `internal/resolver/resolver.go` (+ `_test.go`) | `Service`, `Resolve`, epoch, negative cache, singleflight, matcher provider. |
| `internal/api/server.go` | Shared `atomic.Pointer` live-matcher; `Deps.Resolver`; `reload()` swap. |
| `internal/api/stream.go` (+ `stream_test.go`) | Prefix-discriminate + tri-state + validate boundary. |
| `internal/wiring/wiring.go` | Epoch bump on identity change; async post-swap sweep. |
| `internal/download/manager.go` | `runScan` targeted binding refresh; download link resolve-at-read. |
| `internal/playlistsync/service.go` | Synced library-source tracks resolve-at-read. |
| `cmd/reverb/main.go` | Resolver singleton ownership + provider wiring. |
| `web/src/lib/libraryApi.ts` (+ tests) | Canonical-aware `coverUrl`/`trackCoverUrl` + `?v=`. |
| `web/src/lib/audioEngine.ts` (+ tests) | Stream `error` recovery (active-only, repeat-one, K-cap). |

---

## Phase A — Core identity layer (no user-facing change)

### Task 1: Migration `0019_catalog.sql` + sqlc queries + additive-migration test

**Files:**
- Create: `internal/store/migrations/0019_catalog.sql`
- Create: `internal/store/queries/catalog.sql`
- Modify (generated): `internal/store/db/catalog.sql.go`, `internal/store/db/models.go` (via `make generate`)
- Test: `internal/store/migrate_catalog_test.go`

**Interfaces:**
- Produces: tables `catalog_entity`, `catalog_alias`, `backend_binding` and generated models `db.CatalogEntity`, `db.CatalogAlias`, `db.BackendBinding`; query methods `InsertCatalogEntity`, `GetCatalogEntity`, `InsertCatalogAlias` (ON CONFLICT DO NOTHING), `GetAliasCatalogID`, `ListAliasesForCatalog`, `RepointAliases`, `RepointBindings`, `DeleteCatalogEntity`, `GetBackendBinding`, `UpsertBackendBinding`, `RepointBackendBindingPreferResolved`.

- [ ] **Step 1: Write `0019_catalog.sql`** (exact schema from spec §2; goose Up/Down)

```sql
-- +goose Up
CREATE TABLE catalog_entity (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  artist      TEXT NOT NULL DEFAULT '',
  album       TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  isrc        TEXT NOT NULL DEFAULT '',
  mbid        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT '',
  external_id TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);
CREATE TABLE catalog_alias (
  alias_kind  TEXT NOT NULL,
  alias_value TEXT NOT NULL,
  catalog_id  TEXT NOT NULL REFERENCES catalog_entity(id),
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (alias_kind, alias_value)
);
CREATE INDEX idx_catalog_alias_catalog ON catalog_alias(catalog_id);
CREATE TABLE backend_binding (
  catalog_id       TEXT NOT NULL REFERENCES catalog_entity(id),
  library_identity TEXT NOT NULL,
  backend_id       TEXT NOT NULL DEFAULT '',
  cover_art_id     TEXT NOT NULL DEFAULT '',
  known_absent     INTEGER NOT NULL DEFAULT 0,
  binding_epoch    INTEGER NOT NULL,
  resolved_at      INTEGER NOT NULL,
  PRIMARY KEY (catalog_id, library_identity)
);

-- +goose Down
DROP TABLE backend_binding;
DROP TABLE catalog_alias;
DROP TABLE catalog_entity;
```

- [ ] **Step 2: Write `catalog.sql` queries** (sqlc; names match the Interfaces block)

```sql
-- name: InsertCatalogEntity :exec
INSERT INTO catalog_entity (id, kind, title, artist, album, duration_ms, isrc, mbid, source, external_id, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?);

-- name: GetCatalogEntity :one
SELECT * FROM catalog_entity WHERE id = ?;

-- name: InsertCatalogAlias :exec
INSERT INTO catalog_alias (alias_kind, alias_value, catalog_id, created_at)
VALUES (?,?,?,?) ON CONFLICT(alias_kind, alias_value) DO NOTHING;

-- name: GetAliasCatalogID :one
SELECT catalog_id FROM catalog_alias WHERE alias_kind = ? AND alias_value = ?;

-- name: ListAliasesForCatalog :many
SELECT alias_kind, alias_value FROM catalog_alias WHERE catalog_id = ?;

-- name: RepointAliases :exec
UPDATE catalog_alias SET catalog_id = ? WHERE catalog_id = ?;

-- name: DeleteCatalogEntity :exec
DELETE FROM catalog_entity WHERE id = ?;

-- name: GetBackendBinding :one
SELECT * FROM backend_binding WHERE catalog_id = ? AND library_identity = ?;

-- name: UpsertBackendBinding :exec
INSERT INTO backend_binding (catalog_id, library_identity, backend_id, cover_art_id, known_absent, binding_epoch, resolved_at)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT(catalog_id, library_identity) DO UPDATE SET
  backend_id=excluded.backend_id, cover_art_id=excluded.cover_art_id,
  known_absent=excluded.known_absent, binding_epoch=excluded.binding_epoch, resolved_at=excluded.resolved_at;

-- name: DeleteBindingsForCatalog :exec
DELETE FROM backend_binding WHERE catalog_id = ?;

-- name: RepointBindings :exec
UPDATE backend_binding SET catalog_id = ? WHERE catalog_id = ?;
```
> Merge handles binding PK collisions in Go (Task 4): repoint losers that don't collide, drop losers that do (winner kept). `RepointBindings` + a pre-delete of colliding loser bindings is the mechanism.

- [ ] **Step 3: Generate** — Run `make generate`. Expected: `internal/store/db/catalog.sql.go` + new structs in `models.go`. Verify it builds: `go build ./internal/store/...`

- [ ] **Step 4: Write the additive-migration test** (proves existing rows are untouched + Down/Up round-trips)

```go
package store

import "testing"

// seedLegacyRows inserts real-shaped rows that exist on the live DB BEFORE 0019.
// Use the existing test-store constructor (mirror internal/store/store_test.go setup).
func TestMigration0019_AdditiveAndReversible(t *testing.T) {
	st := newTestStore(t) // applies all migrations through 0019
	ctx := t.Context()

	// 1. Additive: the three new tables exist and are EMPTY.
	for _, tbl := range []string{"catalog_entity", "catalog_alias", "backend_binding"} {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("table %s should start empty, has %d", tbl, n)
		}
	}
	// 2. Pre-existing consumer tables are unaltered (no canonical_id column added).
	rows, err := st.DB().QueryContext(ctx, "SELECT name FROM pragma_table_info('download_jobs')")
	if err != nil { t.Fatal(err) }
	defer rows.Close()
	for rows.Next() {
		var col string
		_ = rows.Scan(&col)
		if col == "canonical_id" {
			t.Fatal("0019 must not add canonical_id to download_jobs in P1")
		}
	}
}
```

- [ ] **Step 5: Run** — `go test ./internal/store/ -run TestMigration0019 -v`. Expected: PASS. (If `newTestStore`/`st.DB()` differ, mirror the exact helper used by neighboring `internal/store` tests.)

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/0019_catalog.sql internal/store/queries/catalog.sql internal/store/db/ internal/store/migrate_catalog_test.go
git commit -m "feat(catalog): additive 0019 catalog/alias/binding tables + queries

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: The `norm` composite fingerprint

**Files:**
- Create: `internal/matching/fingerprint.go`
- Test: `internal/matching/fingerprint_test.go`

**Interfaces:**
- Consumes: existing `Normalize` (`internal/matching/normalize.go`) and the dedup recipe.
- Produces: `func Fingerprint(title, artist, album string, durationMs int) string` — returns a hex sha256 of a structured, qualifier-canonicalized, duration-bucketed key. Used as the `norm` alias value by `catalog.CanonicalFor`.

- [ ] **Step 1: Write failing tests** (the load-bearing cases from spec §3.1)

```go
package matching

import "testing"

func TestFingerprint_LiveVsStudioDistinct(t *testing.T) {
	studio := Fingerprint("Hurt", "Johnny Cash", "American IV", 218000)
	live := Fingerprint("Hurt (Live)", "Johnny Cash", "American IV", 235000)
	if studio == live {
		t.Fatal("live and studio must not collide")
	}
}

func TestFingerprint_QualifierFormsConverge(t *testing.T) {
	// Same live recording labelled three ways must produce ONE fingerprint.
	a := Fingerprint("Song (Live)", "Artist", "Album", 240000)
	b := Fingerprint("Song - Live", "Artist", "Album", 240000)
	c := Fingerprint("Song [Live]", "Artist", "Album", 240000)
	if a != b || b != c {
		t.Fatalf("qualifier forms must converge: %q %q %q", a, b, c)
	}
}

func TestFingerprint_TitleCollisionSeparatedByArtistAlbumDuration(t *testing.T) {
	x := Fingerprint("Intro", "Artist A", "Album A", 60000)
	y := Fingerprint("Intro", "Artist B", "Album B", 95000)
	if x == y {
		t.Fatal("distinct 'Intro' tracks must not collide")
	}
}

func TestFingerprint_DurationWobbleWithinBucketStable(t *testing.T) {
	// <5s wobble (re-match jitter) stays in one bucket.
	a := Fingerprint("Song", "Artist", "Album", 200000)
	b := Fingerprint("Song", "Artist", "Album", 203000)
	if a != b {
		t.Fatal("sub-bucket duration wobble must not split identity")
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/matching/ -run TestFingerprint -v`. Expected: FAIL (`Fingerprint` undefined).

- [ ] **Step 3: Implement `fingerprint.go`**

```go
package matching

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// versionMarkers are recording qualifiers extracted regardless of paren/dash/
// bracket form, so "Song (Live)", "Song - Live", "Song [Live]" converge.
var versionMarkers = []string{"live", "acoustic", "remaster", "remastered", "deluxe", "edit", "remix", "demo", "instrumental", "radio"}

// Fingerprint returns a stable, backend-independent identity key for a track:
// sha256( normArtist ␟ normTitleBase ␟ normAlbum ␟ durationBucket ␟ sortedMarkers ).
// It reuses Normalize so it can never disagree with the matcher's own identity call.
func Fingerprint(title, artist, album string, durationMs int) string {
	nt := Normalize(title)
	markers := extractMarkers(nt)
	for _, m := range markers {
		nt = strings.ReplaceAll(nt, m, "")
	}
	nt = strings.Join(strings.Fields(nt), " ")
	bucket := durationMs / 5000
	var b strings.Builder
	b.WriteString(Normalize(artist))
	b.WriteByte('\x1f')
	b.WriteString(nt)
	b.WriteByte('\x1f')
	b.WriteString(Normalize(album))
	b.WriteByte('\x1f')
	b.WriteString(itoa(bucket))
	b.WriteByte('\x1f')
	b.WriteString(strings.Join(markers, ","))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func extractMarkers(normalized string) []string {
	found := map[string]bool{}
	for _, m := range versionMarkers {
		if strings.Contains(" "+normalized+" ", " "+m+" ") {
			found[m] = true
		}
	}
	out := make([]string, 0, len(found))
	for m := range found {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }
```
> Add `"strconv"` to imports (or inline `strconv.Itoa`). If `Normalize` already keeps `(live)` as a paren token, confirm `extractMarkers` sees `live` after Normalize strips parens to spaces — add a `normalize_test` case proving `Normalize("Song (Live)")` and `Normalize("Song - Live")` both yield a token-separated `live`.

- [ ] **Step 4: Run** — `go test ./internal/matching/ -run 'TestFingerprint|TestNormalize' -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/matching/fingerprint.go internal/matching/fingerprint_test.go internal/matching/normalize_test.go
git commit -m "feat(matching): backend-independent track Fingerprint (qualifier-canonical, duration-bucketed)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: `catalog` package — `Identity`, `Service`, `CanonicalFor` (mint + alias lookup)

**Files:**
- Create: `internal/catalog/catalog.go`, `internal/catalog/canonical.go`
- Test: `internal/catalog/canonical_test.go`

**Interfaces:**
- Consumes: `matching.Fingerprint` (Task 2); generated queries (Task 1).
- Produces:
  - `type Identity struct { Kind, Title, Artist, Album, ISRC, MBID, Source, ExternalID string; DurationMs int }`
  - `type Querier interface { ... }` (the slice of `*db.Queries` catalog needs: `GetAliasCatalogID`, `InsertCatalogEntity`, `InsertCatalogAlias`, `GetCatalogEntity`, plus the merge methods used in Task 4)
  - `func NewService(q Querier, now func() time.Time, idgen func() string) *Service`
  - `func (s *Service) CanonicalFor(ctx context.Context, id Identity) (catalogID string, err error)` — **backend-independent** (pure DB). Mints if no alias matches; always writes a `norm` alias plus `isrc`/`external` aliases when present.

- [ ] **Step 1: Write failing tests**

```go
package catalog

import "testing"

func TestCanonicalFor_MintsAndIsStable(t *testing.T) {
	s := newTestService(t) // wraps a real in-memory store (mirror internal/notification setup)
	ctx := t.Context()
	id := Identity{Kind: "track", Title: "Hurt", Artist: "Johnny Cash", Album: "American IV", DurationMs: 218000}

	c1, err := s.CanonicalFor(ctx, id)
	if err != nil || c1 == "" { t.Fatalf("mint failed: %v", err) }
	if got := c1[:4]; got != "trk_" { t.Fatalf("track id prefix = %q", got) }

	c2, _ := s.CanonicalFor(ctx, id) // same metadata -> same id (norm alias hit)
	if c1 != c2 { t.Fatalf("expected stable id, got %s then %s", c1, c2) }
}

func TestCanonicalFor_ISRCAndNormConverge(t *testing.T) {
	s := newTestService(t)
	ctx := t.Context()
	base := Identity{Kind: "track", Title: "Song", Artist: "Artist", Album: "Album", DurationMs: 200000}
	withISRC := base
	withISRC.ISRC = "GBAAA0000001"

	c1, _ := s.CanonicalFor(ctx, withISRC) // mints with isrc + norm aliases
	c2, _ := s.CanonicalFor(ctx, base)     // no isrc -> norm alias hit -> SAME entity
	if c1 != c2 { t.Fatalf("norm alias should converge: %s vs %s", c1, c2) }
}
```

- [ ] **Step 2: Run** — `go test ./internal/catalog/ -run TestCanonicalFor -v`. Expected: FAIL (package/symbols undefined).

- [ ] **Step 3: Implement `catalog.go` + `canonical.go`**

```go
// catalog.go
package catalog

import (
	"context"
	"time"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type Identity struct {
	Kind, Title, Artist, Album, ISRC, MBID, Source, ExternalID string
	DurationMs int
}

type Querier interface {
	GetAliasCatalogID(ctx context.Context, arg db.GetAliasCatalogIDParams) (string, error)
	InsertCatalogEntity(ctx context.Context, arg db.InsertCatalogEntityParams) error
	InsertCatalogAlias(ctx context.Context, arg db.InsertCatalogAliasParams) error
	GetCatalogEntity(ctx context.Context, id string) (db.CatalogEntity, error)
	// merge methods (Task 4):
	ListAliasesForCatalog(ctx context.Context, catalogID string) ([]db.ListAliasesForCatalogRow, error)
	RepointAliases(ctx context.Context, arg db.RepointAliasesParams) error
	RepointBindings(ctx context.Context, arg db.RepointBindingsParams) error
	DeleteCatalogEntity(ctx context.Context, id string) error
}

type Service struct {
	q     Querier
	now   func() time.Time
	idgen func() string // returns a uuid-ish token (no prefix)
}

func NewService(q Querier, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, now: now, idgen: idgen}
}
```

```go
// canonical.go
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type aliasKV struct{ kind, value string }

func aliasesFor(id Identity) []aliasKV {
	var out []aliasKV
	if id.ISRC != "" { out = append(out, aliasKV{"isrc", id.ISRC}) }
	if id.Source != "" && id.ExternalID != "" {
		out = append(out, aliasKV{"external", id.Source + ":" + id.ExternalID})
	}
	out = append(out, aliasKV{"norm", matching.Fingerprint(id.Title, id.Artist, id.Album, id.DurationMs)})
	return out // priority order: isrc, external, norm
}

func prefixFor(kind string) string {
	switch kind {
	case "album": return "alb_"
	case "artist": return "art_"
	default: return "trk_"
	}
}

// CanonicalFor resolves or mints a catalog id. Backend-independent (pure DB).
func (s *Service) CanonicalFor(ctx context.Context, id Identity) (string, error) {
	aliases := aliasesFor(id)
	// 1. Lookup in priority order.
	for _, a := range aliases {
		cid, err := s.q.GetAliasCatalogID(ctx, db.GetAliasCatalogIDParams{AliasKind: a.kind, AliasValue: a.value})
		if err == nil {
			// Task 4 inserts the remaining aliases (and detects/repairs collisions) here.
			return s.attachAliases(ctx, cid, id, aliases)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	// 2. Mint.
	cid := prefixFor(id.Kind) + s.idgen()
	now := s.now().Unix()
	if err := s.q.InsertCatalogEntity(ctx, db.InsertCatalogEntityParams{
		ID: cid, Kind: id.Kind, Title: id.Title, Artist: id.Artist, Album: id.Album,
		DurationMs: int64(id.DurationMs), Isrc: id.ISRC, Mbid: id.MBID,
		Source: id.Source, ExternalID: id.ExternalID, CreatedAt: now,
	}); err != nil {
		return "", err
	}
	for _, a := range aliases {
		if err := s.q.InsertCatalogAlias(ctx, db.InsertCatalogAliasParams{
			AliasKind: a.kind, AliasValue: a.value, CatalogID: cid, CreatedAt: now,
		}); err != nil {
			return "", err
		}
	}
	return cid, nil
}
```
> `attachAliases` is defined in Task 4 (it inserts any newly-supplied aliases and fires merge on a collision). For Task 3, stub it as: insert the aliases with `InsertCatalogAlias` (ON CONFLICT DO NOTHING) and return `cid` — Task 4 replaces the body with collision handling. Add the stub so Task 3 compiles and passes.

- [ ] **Step 4: Run** — `go test ./internal/catalog/ -run TestCanonicalFor -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/catalog/catalog.go internal/catalog/canonical.go internal/catalog/canonical_test.go
git commit -m "feat(catalog): CanonicalFor — backend-independent mint + alias lookup

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: `catalog` merge — collision detection + corroboration + FK repoint

**Files:**
- Create: `internal/catalog/merge.go`
- Modify: `internal/catalog/canonical.go` (replace the `attachAliases` stub)
- Test: `internal/catalog/merge_test.go`

**Interfaces:**
- Consumes: Task 3 `Service`/`Querier` (incl. the merge query methods); `RepointBindings`/`DeleteBindingsForCatalog` from Task 1.
- Produces: `func (s *Service) attachAliases(ctx, cid string, id Identity, aliases []aliasKV) (string, error)` (collision-aware); internal `func (s *Service) merge(ctx, loser, winner string) error`. After a merge, `CanonicalFor` returns the **winner** id.

- [ ] **Step 1: Write failing tests**

```go
package catalog

import "testing"

func TestCanonicalFor_MergesWhenISRCArrivesLater(t *testing.T) {
	s := newTestService(t); ctx := t.Context()
	// Path A: mint via spotify external id, no ISRC yet.
	a := Identity{Kind:"track", Title:"Song", Artist:"Artist", Album:"Album", DurationMs:200000, Source:"spotify", ExternalID:"SPOTIFY_A"}
	ca, _ := s.CanonicalFor(ctx, a)
	// Path B: SAME track now carries an ISRC AND a different external id; norm matches A.
	b := a; b.ISRC = "GBAAA0000001"; b.ExternalID = "SPOTIFY_B"
	cb, _ := s.CanonicalFor(ctx, b)
	if ca != cb { t.Fatalf("expected merge to a single id, got %s vs %s", ca, cb) }
	// Re-resolving A's original identity now returns the merged (winner) id.
	again, _ := s.CanonicalFor(ctx, a)
	if again != cb { t.Fatalf("post-merge A should resolve to winner: %s vs %s", again, cb) }
}

func TestCanonicalFor_NoMergeWhenISRCCollidesButMetadataDisagrees(t *testing.T) {
	s := newTestService(t); ctx := t.Context()
	x := Identity{Kind:"track", Title:"Completely Different", Artist:"Other", Album:"X", DurationMs:100000, ISRC:"GBDUP0000001"}
	cx, _ := s.CanonicalFor(ctx, x)
	y := Identity{Kind:"track", Title:"Unrelated Song", Artist:"Nobody", Album:"Y", DurationMs:300000, ISRC:"GBDUP0000001"}
	cy, _ := s.CanonicalFor(ctx, y)
	if cx == cy { t.Fatal("duplicate ISRC with disagreeing metadata must NOT merge") }
}
```

- [ ] **Step 2: Run** — `go test ./internal/catalog/ -run 'TestCanonicalFor_Merges|TestCanonicalFor_NoMerge' -v`. Expected: FAIL.

- [ ] **Step 3: Implement `merge.go` + replace `attachAliases`**

```go
// merge.go
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

var aliasPriority = map[string]int{"isrc": 3, "external": 2, "norm": 1, "mbid": 0}

// attachAliases inserts any newly-supplied aliases onto cid; if an alias already
// points at a DIFFERENT entity, that observed collision fires a merge.
func (s *Service) attachAliases(ctx context.Context, cid string, id Identity, aliases []aliasKV) (string, error) {
	winner := cid
	now := s.now().Unix()
	for _, a := range aliases {
		existing, err := s.q.GetAliasCatalogID(ctx, db.GetAliasCatalogIDParams{AliasKind: a.kind, AliasValue: a.value})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := s.q.InsertCatalogAlias(ctx, db.InsertCatalogAliasParams{AliasKind: a.kind, AliasValue: a.value, CatalogID: winner, CreatedAt: now}); err != nil {
				return "", err
			}
		case err != nil:
			return "", err
		case existing != winner:
			// Collision. For an isrc collision, corroborate via norm before merging.
			if a.kind == "isrc" && !s.corroborates(ctx, existing, id) {
				continue // duplicate/re-used ISRC across distinct tracks: do not merge, do not claim alias.
			}
			w, l := pickWinner(winner, existing) // higher-priority anchor wins; here both are entities, pick existing as older
			if err := s.merge(ctx, l, w); err != nil { return "", err }
			winner = w
		}
	}
	return winner, nil
}

func (s *Service) corroborates(ctx context.Context, otherID string, id Identity) bool {
	e, err := s.q.GetCatalogEntity(ctx, otherID)
	if err != nil { return false }
	return matching.Fingerprint(e.Title, e.Artist, e.Album, int(e.DurationMs)) ==
		matching.Fingerprint(id.Title, id.Artist, id.Album, id.DurationMs)
}

// pickWinner returns (winner, loser). The existing (older) entity wins ties.
func pickWinner(newID, existingID string) (string, string) { return existingID, newID }

func (s *Service) merge(ctx context.Context, loser, winner string) error {
	if loser == winner { return nil }
	if err := s.q.RepointAliases(ctx, db.RepointAliasesParams{CatalogID: winner, CatalogID_2: loser}); err != nil { return err }
	// Bindings: repoint losers; on PK collision the winner's binding is kept (delete loser's colliding binding first).
	if err := s.repointBindingsPreferWinner(ctx, loser, winner); err != nil { return err }
	// Stored consumer refs (SP3 plays etc.) repoint via catalogRef helper once those columns exist (P2/SP3).
	return s.q.DeleteCatalogEntity(ctx, loser)
}
```
> `repointBindingsPreferWinner`: read both entities' bindings, for each loser binding whose `(catalog_id=winner, library_identity)` already exists keep the one with a non-empty `backend_id` (else fresher epoch) and delete the loser's; otherwise repoint. Implement with `GetBackendBinding` + `UpsertBackendBinding` + `DeleteBindingsForCatalog`. `RepointAliasesParams` field names come from sqlc (`CatalogID` = new, `CatalogID_2` = old — confirm in generated code).

- [ ] **Step 4: Run** — `go test ./internal/catalog/ -v`. Expected: PASS (all catalog tests).

- [ ] **Step 5: Commit**

```bash
git add internal/catalog/merge.go internal/catalog/canonical.go internal/catalog/merge_test.go
git commit -m "feat(catalog): merge on observed alias collision, ISRC corroboration gate

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: `resolver` package — cache-first Resolve, per-identity epoch, negative cache, singleflight

**Files:**
- Create: `internal/resolver/resolver.go`
- Test: `internal/resolver/resolver_test.go`

**Interfaces:**
- Consumes: generated binding queries (Task 1); the matcher via a `Rematcher` interface; settings KV for identity + epoch.
- Produces:
  - `type Addressing struct { BackendID, CoverArtID string; Found bool }`
  - `type Rematcher interface { Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) }` (satisfied by `*matching.Service`)
  - `type Querier interface { GetCatalogEntity; GetBackendBinding; UpsertBackendBinding; GetSetting; UpsertSetting }`
  - `func NewService(q Querier, matcher func() Rematcher, now func() time.Time) *Service`
  - `func (s *Service) Resolve(ctx context.Context, catalogID string) (Addressing, error)`
  - `func (s *Service) BumpEpoch(ctx context.Context, identity string) error`
  - `func (s *Service) RefreshLinked(ctx context.Context, catalogIDs []string) error`

- [ ] **Step 1: Write failing tests** (cache hit, negative-cache loop bound, epoch invalidation)

```go
package resolver

import (
	"context"
	"testing"
	"github.com/maxjb-xyz/reverb/internal/core"
)

type fakeMatcher struct{ calls int; result core.MatchResult }
func (f *fakeMatcher) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	f.calls++
	return f.result, nil
}

func TestResolve_CachesAndDoesNotRematchOnHit(t *testing.T) {
	s, q, fm := newTestResolver(t) // fm.result = MatchResult{Status: InLibrary, LibraryTrackID:"nav-1", CoverArtID:"al-1"}
	ctx := t.Context()
	cid := seedEntity(t, q, "trk_x", "Song", "Artist", "Album", 200000)

	a1, _ := s.Resolve(ctx, cid)
	if !a1.Found || a1.BackendID != "nav-1" { t.Fatalf("resolve miss: %+v", a1) }
	a2, _ := s.Resolve(ctx, cid)
	if a2.BackendID != "nav-1" { t.Fatal("second resolve wrong") }
	if fm.calls != 1 { t.Fatalf("expected 1 re-match (cached after), got %d", fm.calls) }
}

func TestResolve_NegativeCacheBoundsRematch(t *testing.T) {
	s, q, fm := newTestResolverMiss(t) // fm.result = MatchResult{Status: NotInLibrary}
	ctx := t.Context()
	cid := seedEntity(t, q, "trk_y", "Gone", "Artist", "Album", 200000)
	for i := 0; i < 3; i++ {
		a, _ := s.Resolve(ctx, cid)
		if a.Found { t.Fatal("should be not-found") }
	}
	if fm.calls != 1 { t.Fatalf("known_absent must bound re-match to 1 per epoch, got %d", fm.calls) }
}
```

- [ ] **Step 2: Run** — `go test ./internal/resolver/ -v`. Expected: FAIL.

- [ ] **Step 3: Implement `resolver.go`** (cache-first → negative-cache short-circuit → singleflight re-match → write-back)

```go
package resolver

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
	"golang.org/x/sync/singleflight"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type Addressing struct{ BackendID, CoverArtID string; Found bool }

type Rematcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

type Service struct {
	q       Querier
	matcher func() Rematcher
	now     func() time.Time
	sf      singleflight.Group
}

func NewService(q Querier, matcher func() Rematcher, now func() time.Time) *Service {
	return &Service{q: q, matcher: matcher, now: now}
}

func (s *Service) identity(ctx context.Context) (string, error) {
	return s.q.GetSetting(ctx, "library_identity") // maintained by reconcileLibraryIdentity
}
func (s *Service) epoch(ctx context.Context, identity string) int64 {
	v, err := s.q.GetSetting(ctx, "binding_epoch:"+identity)
	if err != nil { return 1 }
	n, _ := strconv.ParseInt(v, 10, 64)
	if n == 0 { return 1 }
	return n
}

func (s *Service) Resolve(ctx context.Context, catalogID string) (Addressing, error) {
	identity, err := s.identity(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) { return Addressing{}, err }
	curEpoch := s.epoch(ctx, identity)

	b, err := s.q.GetBackendBinding(ctx, db.GetBackendBindingParams{CatalogID: catalogID, LibraryIdentity: identity})
	if err == nil && b.BindingEpoch >= curEpoch {
		if b.KnownAbsent == 1 { return Addressing{Found: false}, nil }   // negative cache
		if b.BackendID != "" { return Addressing{b.BackendID, b.CoverArtID, true}, nil }
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Addressing{}, err
	}

	// Stale/missing: re-match under singleflight keyed by catalog id.
	v, err, _ := s.sf.Do(catalogID, func() (any, error) {
		return s.rematchAndStore(ctx, catalogID, identity, curEpoch)
	})
	if err != nil { return Addressing{}, err }
	return v.(Addressing), nil
}

func (s *Service) rematchAndStore(ctx context.Context, catalogID, identity string, epoch int64) (Addressing, error) {
	e, err := s.q.GetCatalogEntity(ctx, catalogID)
	if err != nil { return Addressing{}, err }
	res, err := s.matcher().Match(ctx, core.ExternalResult{
		Source: e.Source, ExternalID: e.ExternalID, Title: e.Title, Artist: e.Artist,
		Album: e.Album, DurationMs: int(e.DurationMs), ISRC: e.Isrc, MBID: e.Mbid, Type: core.EntityTrack,
	})
	addr := Addressing{}
	bind := db.UpsertBackendBindingParams{CatalogID: catalogID, LibraryIdentity: identity, BindingEpoch: epoch, ResolvedAt: s.now().Unix()}
	if err == nil && res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
		addr = Addressing{res.LibraryTrackID, res.CoverArtID, true}
		bind.BackendID = res.LibraryTrackID; bind.CoverArtID = res.CoverArtID; bind.KnownAbsent = 0
	} else {
		bind.KnownAbsent = 1 // negative cache, stamped at current epoch
	}
	if e := s.q.UpsertBackendBinding(ctx, bind); e != nil { return Addressing{}, e }
	return addr, nil
}

// BumpEpoch (swap reconcile) and RefreshLinked (runScan) — see Task 9.
func (s *Service) BumpEpoch(ctx context.Context, identity string) error {
	next := s.epoch(ctx, identity) + 1
	return s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: "binding_epoch:" + identity, Value: strconv.FormatInt(next, 10)})
}
```
> Add `golang.org/x/sync/singleflight` (likely already in `go.mod`; if not, `go get golang.org/x/sync` — note the dependency in the commit). Confirm `core.MatchInLibrary` / `core.EntityTrack` constant names against `internal/core`. `GetSetting`/`UpsertSetting`/`UpsertSettingParams` are existing generated symbols (used by `internal/api/settings.go`). `RefreshLinked` body: for each id, force `known_absent=0` re-resolve by upserting a stale binding (epoch-1) then `Resolve` — or directly call `rematchAndStore`. Keep it in this file.

- [ ] **Step 4: Run** — `go test ./internal/resolver/ -v`. Expected: PASS. Then `go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/resolver/ go.mod go.sum
git commit -m "feat(resolver): cache-first Resolve with per-identity epoch + negative cache + singleflight

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Wire the resolver singleton + live-matcher provider (server.go + main.go)

**Files:**
- Modify: `internal/api/server.go` (add `liveMatcher atomic.Pointer[...]` updated in `reload()`; add `Deps.Resolver *resolver.Service`; expose `s.resolver()`)
- Modify: `cmd/reverb/main.go` (construct the resolver singleton with a provider reading the live matcher; pass into `Deps`)
- Test: `internal/api/resolver_wiring_test.go`

**Interfaces:**
- Consumes: `resolver.NewService` (Task 5); the matcher built in `wiring.Builder.Build`.
- Produces: `Deps.Resolver` available to handlers; a `func() resolver.Rematcher` provider that always returns the **current** matcher (never a stale one).

- [ ] **Step 1: Write failing test** — provider returns the post-reload matcher.

```go
package api

import "testing"

func TestResolverProvider_FollowsReload(t *testing.T) {
	// Build a server with matcher M1; assert provider() == M1.
	// Trigger reload() swapping to M2; assert provider() == M2 (no stale capture).
	// (Mirror the existing reload test setup in internal/api/*_test.go.)
}
```

- [ ] **Step 2: Run** — `go test ./internal/api/ -run TestResolverProvider -v`. Expected: FAIL.

- [ ] **Step 3: Implement** — In `server.go`: add `liveMatcher atomic.Pointer[matcherHolder]` (a tiny holder struct wrapping `resolver.Rematcher`, since `atomic.Pointer` needs a concrete type). `reload()` (line 181) and initial wiring set it whenever the bundle's matcher changes. The provider passed to `resolver.NewService` is `func() resolver.Rematcher { return s.liveMatcher.Load().m }`. In `main.go`, build the resolver once (singleton), give it the provider, store on `Deps.Resolver`. Because the resolver is constructed once but reads the live matcher per call, it survives reloads.
> The matcher (`*matching.Service`) is currently built inside `wiring.Builder.Build` (two instances). Expose the aggregator's matcher on the returned `ServiceBundle` so `reload()` can publish it into `liveMatcher`. Add a `Matcher resolver.Rematcher` field to the bundle.

- [ ] **Step 4: Run** — `go test ./internal/api/ -run TestResolverProvider -v && go build ./...`. Expected: PASS / builds.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go cmd/reverb/main.go internal/wiring/wiring.go internal/api/resolver_wiring_test.go
git commit -m "feat(resolver): singleton resolver + live-matcher provider surviving reloads

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase B — Addressing boundary + reconcile

### Task 7: Cover/stream boundary — prefix-discriminate + tri-state + validate

**Files:**
- Modify: `internal/api/stream.go` (`handleStream`, `handleCover`)
- Test: `internal/api/stream_test.go`

**Interfaces:**
- Consumes: `Deps.Resolver.Resolve` (Task 5); existing `libraryReady`, `lib.Stream`, `lib.CoverArt`.
- Produces: handlers that serve BOTH a canonical id (`trk_/alb_/art_`) and a raw backend id (live-browse passthrough).

- [ ] **Step 1: Write failing tests**

```go
package api

import ("net/http"; "net/http/httptest"; "testing")

func TestHandleCover_BackendIdPassesThrough(t *testing.T) {
	// id without canonical prefix -> adapter called directly, resolver NOT invoked (no catalog pollution).
}
func TestHandleCover_CanonicalKnownAbsent404sWithoutBackendCall(t *testing.T) {
	// resolver returns Found=false -> 404 and the adapter is never called.
}
func TestHandleCover_CanonicalResolvesThenServes(t *testing.T) {
	// resolver returns Found=true backend_id -> adapter serves the bytes; ?size= threaded.
}
```

- [ ] **Step 2: Run** — `go test ./internal/api/ -run TestHandleCover -v`. Expected: FAIL.

- [ ] **Step 3: Implement** — extract a helper and branch on prefix BEFORE the resolver:

```go
func isCanonicalID(id string) bool {
	return strings.HasPrefix(id, "trk_") || strings.HasPrefix(id, "alb_") || strings.HasPrefix(id, "art_")
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w); if !ok { return }
	id := chi.URLParam(r, "id")
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if isCanonicalID(id) {
		addr, err := s.deps.Resolver.Resolve(r.Context(), id)
		if err != nil { writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()}); return }
		if !addr.Found || addr.CoverArtID == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}); return // tri-state: never forward
		}
		id = addr.CoverArtID
	}
	s.serveCover(w, r, lib, id, size) // existing body, extracted; keeps the adapter's non-image validate + Cache-Control
}
```
> Apply the symmetric change to `handleStream` (resolve → `addr.BackendID`, thread `Range`). Keep the adapter's existing non-image/non-audio reject as the validate step. Change the cover `Cache-Control` line to append a version segment OR drop to `max-age=300` (spec §6.4); simplest: keep `max-age=86400` but have `coverUrl()` append `?v=<library_version>` in Task 9-FE so the URL busts on swap. For a `trk_` cover, `addr.CoverArtID` is the album cover the matcher returned — no `alb_` lookup.

- [ ] **Step 4: Run** — `go test ./internal/api/ -run 'TestHandleCover|TestHandleStream' -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/stream.go internal/api/stream_test.go
git commit -m "feat(api): canonical-aware cover/stream boundary (prefix-discriminate, tri-state, validate)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: Reconcile — epoch bump on swap + targeted refresh in runScan + async sweep

**Files:**
- Modify: `internal/wiring/wiring.go` (`reconcileLibraryIdentity`: also `Resolver.BumpEpoch`; enqueue async sweep)
- Modify: `internal/download/manager.go` (`runScan`: `Resolver.RefreshLinked` for just-linked ids)
- Test: `internal/wiring/wiring_test.go`, `internal/download/manager_test.go`

**Interfaces:**
- Consumes: `Resolver.BumpEpoch`, `Resolver.RefreshLinked` (Task 5).
- Produces: a swap bumps the per-identity epoch (invalidating bindings) exactly once; a scan refreshes only the canonical ids it touched.

- [ ] **Step 1: Write failing tests** — (a) identity change bumps epoch once + is idempotent; (b) a plain scan does NOT bump the binding epoch (only `library_version`).

```go
func TestReconcile_IdentityChangeBumpsBindingEpochOnce(t *testing.T) { /* ... */ }
func TestRunScan_DoesNotBumpBindingEpoch(t *testing.T) { /* only library_version moves; bindings stay valid */ }
```

- [ ] **Step 2: Run** — `go test ./internal/wiring/ ./internal/download/ -run 'TestReconcile_Identity|TestRunScan_DoesNot' -v`. Expected: FAIL.

- [ ] **Step 3: Implement** — In `reconcileLibraryIdentity` (wiring.go:300), when identity differs, after the existing `library_version` bump, call `b.resolver.BumpEpoch(ctx, newIdentity)` and enqueue a bounded async sweep (`go b.resolver.RefreshLinked(ctx, visibleCatalogIDs)` gathered from open requests / sync-enabled playlists / completed-unlinked jobs — keep the gather query small and bounded). In `runScan` (manager.go ~919) after re-matching, call `resolver.RefreshLinked(ctx, linkedCatalogIDs)`. The resolver reaches `wiring.Builder`/`manager` via a new field (pass it in `NewBuilder`/`NewManager`, mirroring how `VersionStore`/`bus` are passed).

- [ ] **Step 4: Run** — `go test ./internal/wiring/ ./internal/download/ -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/wiring/wiring.go internal/download/manager.go internal/wiring/wiring_test.go internal/download/manager_test.go
git commit -m "feat(resolver): epoch bump on backend swap + targeted binding refresh on scan + async sweep

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 9: FE — canonical-aware cover URLs + audioEngine stream-error recovery

**Files:**
- Modify: `web/src/lib/libraryApi.ts`, `web/src/lib/audioEngine.ts`
- Test: `web/src/lib/libraryApi.test.ts`, `web/src/lib/audioEngine.test.ts`

**Interfaces:**
- Consumes: the canonical-aware boundary (Task 7).
- Produces: `coverUrl(id)` appends `?v=<libraryVersion>`; `trackCoverUrl(track)` emits `coverUrl(track.canonicalId ?? track.albumId ?? track.coverArtId)`; `audioEngine` recovers from a dead stream.

- [ ] **Step 1: Write failing tests** (Vitest)

```ts
import { describe, it, expect } from 'vitest'
import { coverUrl } from './libraryApi'

describe('coverUrl', () => {
  it('appends a version segment so swaps bust the cache', () => {
    expect(coverUrl('alb_123', 300, 7)).toContain('v=7')
  })
})
```
For `audioEngine.test.ts`: drive the existing injectable `AudioElement`, fire an `'error'` event on the active element, and assert: (a) `repeat==='one'` does NOT advance the queue; (b) otherwise it advances; (c) after 3 consecutive load errors it stops (`playing===false`) rather than looping.

- [ ] **Step 2: Run** — `cd web && npx vitest run src/lib/libraryApi.test.ts src/lib/audioEngine.test.ts`. Expected: FAIL.

- [ ] **Step 3: Implement** — `coverUrl(id, size=300, v?)` appends `&v=${v}` when provided (thread `libraryVersion` from the app store where covers render). `audioEngine`: in `bindActive`, add `this.active.addEventListener('error', this.onError)`. `onError`: if `repeat==='one'` reload once then stop; else increment a `consecutiveErrors` counter and `advance(1, true)`, but if `consecutiveErrors >= 3` set `playing=false`, emit, and reset. Reset the counter on a successful `timeupdate`/`play`. Do NOT bind `error` on the preload element (null its `src` on a preload error).

- [ ] **Step 4: Run** — `cd web && npx vitest run src/lib/libraryApi.test.ts src/lib/audioEngine.test.ts && npx tsc --noEmit`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/libraryApi.ts web/src/lib/audioEngine.ts web/src/lib/libraryApi.test.ts web/src/lib/audioEngine.test.ts
git commit -m "feat(web): canonical-aware cover URLs (?v=) + audioEngine stream-error recovery

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase C — The two wrong-answer-on-swap consumers

### Task 10: Synced-playlist library-source tracks — resolve at read

**Files:**
- Modify: `internal/playlistsync/service.go` (the `source=='library'` branch in `Detail`, ~`:166-179`)
- Test: `internal/playlistsync/service_test.go`

**Interfaces:**
- Consumes: `catalog.CanonicalFor` (mint at add time from the library track's metadata) + `Resolver.Resolve` (resolve at read).
- Produces: a synced library-source track renders the CURRENT backend cover/play target after a swap, not a frozen dead id.

- [ ] **Step 1: Write failing test** — add a library-source track, simulate a swap (bump epoch + change the fake adapter's id mapping), assert `Detail` returns the new backend id/cover, not the frozen one.

- [ ] **Step 2: Run** — `go test ./internal/playlistsync/ -run TestDetail_LibrarySourceResolvesAfterSwap -v`. Expected: FAIL.

- [ ] **Step 3: Implement** — At add time, store the track's `catalog_id` (mint via `CanonicalFor` from `tr.Title/Artist/Album/ISRC/DurationMs`) in the `tracks_json` entry. In `Detail`, for `source=='library'` entries, call `Resolver.Resolve(ctx, catalogID)` and use `addr.BackendID`/`addr.CoverArtID` (mirroring the non-library branch's live behavior) instead of the frozen `tr.ExternalID`/`tr.CoverArtID`. Fall back to the frozen value only when `catalogID` is absent (older rows) — those self-heal once re-saved.

- [ ] **Step 4: Run** — `go test ./internal/playlistsync/ -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/playlistsync/service.go internal/playlistsync/service_test.go
git commit -m "fix(playlistsync): resolve synced library-source tracks at read so covers/playback survive swaps

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 11: Download-job links — resolve at read instead of clear-and-rematch on swap

**Files:**
- Modify: `internal/download/manager.go` (where a completed job's `library_track_id`/`cover_art_id` are read for display/playlist-add)
- Test: `internal/download/manager_test.go`

**Interfaces:**
- Consumes: `catalog.CanonicalFor` (mint from the job's durable metadata) + `Resolver.Resolve`.
- Produces: a completed download's play target/cover is correct after a plain re-scan (today only the swap path clears it).

- [ ] **Step 1: Write failing test** — a completed job; simulate a within-backend rescan that moves its id (no identity change); assert the job's resolved play target follows, without the swap-only clear.

- [ ] **Step 2: Run** — `go test ./internal/download/ -run TestDownload_LinkResolvesAfterRescan -v`. Expected: FAIL.

- [ ] **Step 3: Implement** — when surfacing a completed job's library link, mint/resolve via `CanonicalFor`+`Resolver.Resolve(ctx, catalogID)` from the job's stored `(source, external_id, isrc, title, artist, album)` rather than trusting the stored `library_track_id`. Keep the stored column as a cache, but treat the resolver as the source of truth. (This makes the swap-only `ClearMatchedDownloadJobLibraryRefs` dance unnecessary; leave it in place for P1 — its removal is P2.)

- [ ] **Step 4: Run** — `go test ./internal/download/ -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/download/manager.go internal/download/manager_test.go
git commit -m "fix(download): resolve completed-job library links at read (survives plain rescan)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 12: Full gate + whole-branch review

- [ ] **Step 1: Run the full backend gate**

Run: `go test ./... && go build ./... && go vet ./...`
Expected: all PASS.

- [ ] **Step 2: Run the full FE gate**

Run: `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`
Expected: vitest green, tsc clean, build succeeds, e2e 25/25 (re-run once if `playlist-sync.spec.ts` flakes with `net::ERR_ABORTED`).

- [ ] **Step 3: Whole-branch review** — Use `superpowers:requesting-code-review` across the full `feat/library-identity-foundation` diff (spec adherence, the resolver race/loop-safety, migration additivity, no catalog pollution from live-browse, FE token compliance). Address findings.

- [ ] **Step 4: Fast-forward merge to local `main`** (do NOT push):

```bash
git checkout main && git merge --ff-only feat/library-identity-foundation
```

- [ ] **Step 5:** Tell the user P1 is merged to local `main` and ready for them to **push + rebuild + verify on `soulkiller`** (covers/playlists/playback survive a swap) before SP3 begins.

---

## Notes for the implementer
- **Test setup:** mirror the in-memory store + service construction used in `internal/notification/*_test.go` and `internal/matching/*_test.go`. Where this plan references `newTestStore`/`newTestService`/`seedEntity`, create small local helpers following those patterns.
- **sqlc param names:** generated `...Params` field names (e.g. `RepointAliasesParams.CatalogID_2`) come from the column/arg order — confirm against the generated `catalog.sql.go` after `make generate` and adjust call sites.
- **Don't gold-plate:** P1 leaves `match_cache`, `album_external_map`/`artist_external_map`, and `requests.cover_art_id` untouched (they self-heal or have a `cover_url` fallback) — they are P2. Don't migrate them here.
