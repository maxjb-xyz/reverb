# Multi-User: Accounts & Identity Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn Reverb from single-admin into multi-user — real user accounts, code-defined capabilities bundled into admin-composable roles, sessions tied to a user, and the invariant that every authenticated request resolves to exactly one current user.

**Architecture:** A capability *registry* in Go is the enforcement contract; `roles` (DB rows) bundle capability keys as a JSON array; `users` reference one role. `auth.Service` is the single seam over storage (JSON never leaks to callers). Middleware resolves the session → `CurrentUser` (with a resolved capability set) into request context; `requireCapability`/`requireAdmin` gate routes. A startup seed creates the three system roles and migrates an existing single-admin install into the owner account. Auth-disabled mode is removed. Frontend hydrates current-user + capabilities from `/me` and gates UI defense-in-depth.

**Tech Stack:** Go (chi, modernc sqlite, goose migrations, sqlc, bcrypt, google/uuid), React 19 + TS (Vite, Tailwind design tokens, TanStack Query, Zustand, Playwright).

## Global Constraints

- **Generated code is generated.** `internal/store/db/*.sql.go` + `models.go` are owned by sqlc — edit `.sql` queries then run `make gen`; never hand-edit generated files.
- **Migrations are append-only.** New goose file `internal/store/migrations/0013_*.sql`; never edit applied migrations `0001`–`0012`.
- **Branch + green gate before merge.** Root: `go test ./...` && `go build ./...` && `go vet ./...`. From `web/`: `npx vitest run` && `npx tsc --noEmit` && `npm run build` && `npm run e2e` (e2e stays green). Never `git push` without explicit go-ahead; merge only to local `main`.
- **Design tokens only** (frontend): no raw hex, no `text-black`/`text-white`; accent is red, `text-on-accent` on accent surfaces. Match existing component density/idioms.
- **Capability keys (the fixed registry):** `is_admin`, `can_manage_users`, `can_manage_library`, `can_download`, `can_request`, `can_create_playlists`.
- **System role IDs (fixed, for stable references):** `role-admin`, `role-user`, `role-requester`.
- **Default role bundles:** Admin = all six caps; User = `can_download,can_request,can_create_playlists`; Requester = `can_request`.
- **Commit footer:** end every commit with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Reuse existing route paths:** `/setup/status`, `/setup/admin`, `/auth/login`, `/auth/logout`, `/me` already exist — modify them, don't add parallel `/auth/*` variants.

---

## Phase 1 — Backend identity core

### Task 1: Capability registry

**Files:**
- Create: `internal/auth/capabilities.go`
- Test: `internal/auth/capabilities_test.go`

**Interfaces:**
- Produces:
  - `type Capability struct { Key string; Label string }`
  - `func AllCapabilities() []Capability` — registry in stable display order.
  - `func IsCapability(key string) bool`
  - `func ValidateCapabilities(keys []string) error` — returns `ErrInvalidCapability` if any key is unknown.
  - Const keys: `CapAdmin="is_admin"`, `CapManageUsers="can_manage_users"`, `CapManageLibrary="can_manage_library"`, `CapDownload="can_download"`, `CapRequest="can_request"`, `CapCreatePlaylists="can_create_playlists"`.
  - `func DefaultSystemRoles() []SeedRole` where `type SeedRole struct { ID, Name string; IsSystem bool; Capabilities []string }` — the three seeds with fixed IDs.
  - `var ErrInvalidCapability = errors.New("unknown capability")`

- [ ] **Step 1: Write the failing test**

```go
package auth

import "testing"

func TestAllCapabilitiesContainsKnownKeys(t *testing.T) {
	caps := AllCapabilities()
	if len(caps) != 6 {
		t.Fatalf("want 6 capabilities, got %d", len(caps))
	}
	want := map[string]bool{CapAdmin: false, CapManageUsers: false, CapManageLibrary: false, CapDownload: false, CapRequest: false, CapCreatePlaylists: false}
	for _, c := range caps {
		if _, ok := want[c.Key]; !ok {
			t.Errorf("unexpected capability %q", c.Key)
		}
		if c.Label == "" {
			t.Errorf("capability %q has empty label", c.Key)
		}
		want[c.Key] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing capability %q", k)
		}
	}
}

func TestValidateCapabilities(t *testing.T) {
	if err := ValidateCapabilities([]string{CapDownload, CapRequest}); err != nil {
		t.Fatalf("valid caps rejected: %v", err)
	}
	if err := ValidateCapabilities([]string{"can_teleport"}); err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestDefaultSystemRoles(t *testing.T) {
	roles := DefaultSystemRoles()
	byID := map[string]SeedRole{}
	for _, r := range roles {
		byID[r.ID] = r
	}
	admin, ok := byID["role-admin"]
	if !ok || len(admin.Capabilities) != 6 || !admin.IsSystem {
		t.Fatalf("admin seed wrong: %+v", admin)
	}
	user := byID["role-user"]
	if len(user.Capabilities) != 3 {
		t.Errorf("user seed should have 3 caps, got %d", len(user.Capabilities))
	}
	req := byID["role-requester"]
	if len(req.Capabilities) != 1 || req.Capabilities[0] != CapRequest {
		t.Errorf("requester seed wrong: %+v", req)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestAllCapabilities -v`
Expected: FAIL (undefined: AllCapabilities).

- [ ] **Step 3: Write the implementation**

```go
package auth

import "errors"

const (
	CapAdmin           = "is_admin"
	CapManageUsers     = "can_manage_users"
	CapManageLibrary   = "can_manage_library"
	CapDownload        = "can_download"
	CapRequest         = "can_request"
	CapCreatePlaylists = "can_create_playlists"
)

var ErrInvalidCapability = errors.New("unknown capability")

type Capability struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// AllCapabilities is the fixed registry, in display order. Adding a capability
// here is the only way to introduce one; it is the enforcement contract.
func AllCapabilities() []Capability {
	return []Capability{
		{CapAdmin, "Administrator"},
		{CapManageUsers, "Manage users"},
		{CapManageLibrary, "Manage library & integrations"},
		{CapDownload, "Download tracks"},
		{CapRequest, "Request tracks"},
		{CapCreatePlaylists, "Create playlists"},
	}
}

func IsCapability(key string) bool {
	for _, c := range AllCapabilities() {
		if c.Key == key {
			return true
		}
	}
	return false
}

func ValidateCapabilities(keys []string) error {
	for _, k := range keys {
		if !IsCapability(k) {
			return ErrInvalidCapability
		}
	}
	return nil
}

type SeedRole struct {
	ID           string
	Name         string
	IsSystem     bool
	Capabilities []string
}

func DefaultSystemRoles() []SeedRole {
	return []SeedRole{
		{ID: "role-admin", Name: "Admin", IsSystem: true, Capabilities: []string{
			CapAdmin, CapManageUsers, CapManageLibrary, CapDownload, CapRequest, CapCreatePlaylists,
		}},
		{ID: "role-user", Name: "User", IsSystem: true, Capabilities: []string{
			CapDownload, CapRequest, CapCreatePlaylists,
		}},
		{ID: "role-requester", Name: "Requester", IsSystem: true, Capabilities: []string{
			CapRequest,
		}},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/capabilities.go internal/auth/capabilities_test.go
git commit -m "feat(auth): capability registry + default system role seeds"
```

---

### Task 2: Schema migration + sqlc queries

**Files:**
- Create: `internal/store/migrations/0013_users_roles_invites.sql`
- Create: `internal/store/queries/roles.sql`, `internal/store/queries/users.sql`, `internal/store/queries/invites.sql`
- Modify: `internal/store/queries/sessions.sql`
- Regenerate: `internal/store/db/*` via `make gen`
- Test: `internal/store/store_test.go` (append a round-trip test)

**Interfaces:**
- Produces (sqlc-generated, names follow existing `-- name:` style): `CountUsers`, `CreateUser`, `GetUserByID`, `GetUserByUsername`, `ListUsers`, `UpdateUserRole`, `SetUserDisabled`, `SetUserPassword`, `DeleteUser`, `TouchUserLastSeen`; `ListRoles`, `GetRole`, `CreateRole`, `UpdateRole`, `DeleteRole`, `CountUsersWithRole`; `CreateInvite`, `GetInviteByCode`, `ListInvites`, `MarkInviteUsed`, `DeleteInvite`; session changes `CreateSession` (now takes `user_id`), `GetSession` (returns `user_id`), `DeleteSessionsForUserExcept`, `BackfillSessionUser`.

- [ ] **Step 1: Write the migration**

```sql
-- internal/store/migrations/0013_users_roles_invites.sql
-- +goose Up
CREATE TABLE roles (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    is_system    INTEGER NOT NULL DEFAULT 0,
    capabilities TEXT NOT NULL DEFAULT '[]',
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at   INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role_id       TEXT NOT NULL REFERENCES roles(id),
    is_owner      INTEGER NOT NULL DEFAULT 0,
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    last_seen     INTEGER
);

CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    role_id    TEXT REFERENCES roles(id),
    created_by TEXT REFERENCES users(id),
    expires_at INTEGER,
    used_by    TEXT REFERENCES users(id),
    used_at    INTEGER,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

ALTER TABLE sessions       ADD COLUMN user_id       TEXT REFERENCES users(id);
ALTER TABLE download_jobs  ADD COLUMN initiated_by  TEXT REFERENCES users(id);
ALTER TABLE synced_playlists ADD COLUMN owner_user_id TEXT REFERENCES users(id);

-- +goose Down
ALTER TABLE synced_playlists DROP COLUMN owner_user_id;
ALTER TABLE download_jobs    DROP COLUMN initiated_by;
ALTER TABLE sessions         DROP COLUMN user_id;
DROP TABLE invites;
DROP TABLE users;
DROP TABLE roles;
```

- [ ] **Step 2: Write the query files**

```sql
-- internal/store/queries/roles.sql
-- name: ListRoles :many
SELECT * FROM roles ORDER BY is_system DESC, name;

-- name: GetRole :one
SELECT * FROM roles WHERE id = ?;

-- name: CreateRole :exec
INSERT INTO roles (id, name, is_system, capabilities) VALUES (?, ?, ?, ?);

-- name: UpdateRole :exec
UPDATE roles SET name = ?, capabilities = ?, updated_at = unixepoch() WHERE id = ?;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = ?;

-- name: CountUsersWithRole :one
SELECT COUNT(*) FROM users WHERE role_id = ?;
```

```sql
-- internal/store/queries/users.sql
-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateUser :exec
INSERT INTO users (id, username, password_hash, role_id, is_owner) VALUES (?, ?, ?, ?, ?);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ? COLLATE NOCASE;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at;

-- name: UpdateUserRole :exec
UPDATE users SET role_id = ?, updated_at = unixepoch() WHERE id = ?;

-- name: SetUserDisabled :exec
UPDATE users SET disabled = ?, updated_at = unixepoch() WHERE id = ?;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = ?, updated_at = unixepoch() WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: TouchUserLastSeen :exec
UPDATE users SET last_seen = unixepoch() WHERE id = ?;
```

```sql
-- internal/store/queries/invites.sql
-- name: CreateInvite :exec
INSERT INTO invites (id, code, role_id, created_by, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: GetInviteByCode :one
SELECT * FROM invites WHERE code = ?;

-- name: ListInvites :many
SELECT * FROM invites ORDER BY created_at DESC;

-- name: MarkInviteUsed :exec
UPDATE invites SET used_by = ?, used_at = unixepoch() WHERE id = ?;

-- name: DeleteInvite :exec
DELETE FROM invites WHERE id = ?;
```

Append to `internal/store/queries/sessions.sql` (replace `CreateSession`, `GetSession`; add two):

```sql
-- name: CreateSession :exec
INSERT INTO sessions (id, token_hash, user_id, expires_at) VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: DeleteSessionsForUserExcept :exec
DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?;

-- name: BackfillSessionUser :exec
UPDATE sessions SET user_id = ? WHERE user_id IS NULL;
```

- [ ] **Step 3: Regenerate sqlc and build**

Run: `make gen && go build ./...`
Expected: regenerates `internal/store/db/{roles,users,invites,sessions}.sql.go` + `models.go`; builds clean. (Existing `auth.go`/`handlers.go` call sites for `CreateSession` will now fail to compile because the signature changed — that is expected and fixed in Task 3/Task 5. To keep this task independently green, temporarily update the two existing `CreateSession` callers in `internal/auth/auth.go` to pass `uuid.NewString()`'s sibling `user_id` as `""`; Task 3 replaces this logic.) If you prefer a clean gate, complete Steps 4–5 of this task together with Task 3 in one commit; the reviewer may accept the merged commit.

- [ ] **Step 4: Write the round-trip test**

```go
// append to internal/store/store_test.go
func TestUsersRolesRoundTrip(t *testing.T) {
	st := newTestStore(t) // existing helper in this file
	ctx := context.Background()
	q := st.Q()
	if err := q.CreateRole(ctx, db.CreateRoleParams{ID: "role-user", Name: "User", IsSystem: 1, Capabilities: `["can_request"]`}); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateUser(ctx, db.CreateUserParams{ID: "u1", Username: "alice", PasswordHash: "h", RoleID: "role-user", IsOwner: 0}); err != nil {
		t.Fatal(err)
	}
	u, err := q.GetUserByUsername(ctx, "ALICE") // NOCASE
	if err != nil || u.ID != "u1" {
		t.Fatalf("case-insensitive lookup failed: %v %+v", err, u)
	}
	n, _ := q.CountUsers(ctx)
	if n != 1 {
		t.Fatalf("want 1 user, got %d", n)
	}
}
```

(If `newTestStore` does not exist, mirror the existing setup pattern at the top of `store_test.go`; reuse whatever helper the file already uses to open a migrated in-memory/temp store.)

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/store/ -run TestUsersRolesRoundTrip -v`
Expected: PASS.

```bash
git add internal/store/migrations/0013_users_roles_invites.sql internal/store/queries/ internal/store/db/ internal/store/store_test.go
git commit -m "feat(store): users/roles/invites schema + queries + session user_id"
```

---

### Task 3: Auth service — sessions-with-user, login, setup, capability resolution

**Files:**
- Modify: `internal/auth/auth.go`
- Modify: `internal/auth/auth_test.go`

**Interfaces:**
- Consumes: Task 1 (`CapAdmin`, `DefaultSystemRoles`, `ValidateCapabilities`), Task 2 (generated queries).
- Produces:
  - `type CurrentUser struct { ID, Username, RoleID, RoleName string; IsOwner bool; Caps map[string]bool }` with `func (u CurrentUser) Has(cap string) bool { return u.Caps[cap] }`.
  - `func (s *Service) IsSetupRequired(ctx) (bool, error)` — now `CountUsers()==0`.
  - `func (s *Service) SetupOwner(ctx, username, password string) (userID string, err error)`.
  - `func (s *Service) Login(ctx, username, password string) (userID string, err error)` — `ErrInvalidCreds`/`ErrUserDisabled`.
  - `func (s *Service) CreateSession(ctx, userID string) (token string, err error)` (signature changes — now takes userID).
  - `func (s *Service) ResolveSession(ctx, token string) (CurrentUser, error)` (replaces `ValidateToken`).
  - `func (s *Service) resolveCaps(ctx, roleID string) (map[string]bool, string, error)` (private helper).
  - Errors: `ErrInvalidCreds`, `ErrUserDisabled`.
  - The `Querier` interface grows to include the Task-2 user/role queries.
  - **Remove:** `IsAuthDisabled`, `SetAuthDisabled`, `SetAdminPassword`, `CheckLogin`, the old `ValidateToken`, and the `keyAuthDisabled`/`keyAdminHash` auth paths (keep `keyAdminHash` const only if the seed in Task 4 still reads it).

- [ ] **Step 1: Write failing tests**

```go
// internal/auth/auth_test.go — add (keep existing hash tests)
func TestSetupOwnerThenLogin(t *testing.T) {
	s, _ := newTestService(t) // helper that wires a migrated store + seeded system roles
	ctx := context.Background()
	uid, err := s.SetupOwner(ctx, "owner", "pw12345")
	if err != nil {
		t.Fatal(err)
	}
	if req, _ := s.IsSetupRequired(ctx); req {
		t.Fatal("setup should no longer be required")
	}
	got, err := s.Login(ctx, "OWNER", "pw12345") // username is case-insensitive
	if err != nil || got != uid {
		t.Fatalf("login failed: %v %s", err, got)
	}
	if _, err := s.Login(ctx, "owner", "wrong"); err != ErrInvalidCreds {
		t.Fatalf("want ErrInvalidCreds, got %v", err)
	}
}

func TestResolveSessionCarriesCaps(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	uid, _ := s.SetupOwner(ctx, "owner", "pw12345")
	tok, err := s.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	cu, err := s.ResolveSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if cu.ID != uid || !cu.IsOwner || !cu.Has(CapAdmin) {
		t.Fatalf("owner session wrong: %+v", cu)
	}
	if _, err := s.ResolveSession(ctx, "garbage"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}
```

`newTestService` helper (add to the test file): open a temp store via the store package, run `Migrate()`, call the Task-4 `EnsureSeed`, construct `NewService(store.Q(), time.Now)`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/ -run TestSetupOwner -v`
Expected: FAIL (undefined SetupOwner).

- [ ] **Step 3: Implement**

Replace the auth-disabled/admin-password machinery with user-based logic. Key bodies:

```go
var (
	ErrInvalidCreds = errors.New("invalid credentials")
	ErrUserDisabled = errors.New("user disabled")
)

type CurrentUser struct {
	ID       string          `json:"id"`
	Username string          `json:"username"`
	RoleID   string          `json:"roleId"`
	RoleName string          `json:"roleName"`
	IsOwner  bool            `json:"isOwner"`
	Caps     map[string]bool `json:"-"`
}

func (u CurrentUser) Has(cap string) bool { return u.Caps[cap] }

func (s *Service) IsSetupRequired(ctx context.Context) (bool, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *Service) SetupOwner(ctx context.Context, username, password string) (string, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", errors.New("setup already complete")
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	if err := s.q.CreateUser(ctx, db.CreateUserParams{
		ID: id, Username: username, PasswordHash: h, RoleID: "role-admin", IsOwner: 1,
	}); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	u, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		return "", ErrInvalidCreds
	}
	if u.Disabled == 1 {
		return "", ErrUserDisabled
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return "", ErrInvalidCreds
	}
	return u.ID, nil
}

func (s *Service) CreateSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID: uuid.NewString(), TokenHash: hashToken(tok), UserID: sql.NullString{String: userID, Valid: true}, ExpiresAt: s.now().Add(sessionTTL).Unix(),
	}); err != nil {
		return "", err
	}
	return tok, nil
}

func (s *Service) ResolveSession(ctx context.Context, tok string) (CurrentUser, error) {
	if tok == "" {
		return CurrentUser{}, errors.New("no token")
	}
	sess, err := s.q.GetSession(ctx, hashToken(tok))
	if err != nil || sess.ExpiresAt < s.now().Unix() || !sess.UserID.Valid {
		return CurrentUser{}, errors.New("invalid session")
	}
	u, err := s.q.GetUserByID(ctx, sess.UserID.String)
	if err != nil || u.Disabled == 1 {
		return CurrentUser{}, errors.New("invalid user")
	}
	caps, roleName, err := s.resolveCaps(ctx, u.RoleID)
	if err != nil {
		return CurrentUser{}, err
	}
	_ = s.q.TouchUserLastSeen(ctx, u.ID)
	return CurrentUser{ID: u.ID, Username: u.Username, RoleID: u.RoleID, RoleName: roleName, IsOwner: u.IsOwner == 1, Caps: caps}, nil
}

func (s *Service) resolveCaps(ctx context.Context, roleID string) (map[string]bool, string, error) {
	r, err := s.q.GetRole(ctx, roleID)
	if err != nil {
		return nil, "", err
	}
	var keys []string
	if err := json.Unmarshal([]byte(r.Capabilities), &keys); err != nil {
		return nil, "", err
	}
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m, r.Name, nil
}
```

Add imports: `database/sql`, `encoding/json`. Extend the `Querier` interface with the new query methods used above (`CountUsers`, `CreateUser`, `GetUserByUsername`, `GetUserByID`, `GetRole`, `TouchUserLastSeen`, plus those used in Tasks 4/6/7/8/9). Delete `IsAuthDisabled`, `SetAuthDisabled`, `CheckLogin`, `SetAdminPassword`, old `ValidateToken`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat(auth): user-based sessions, login, setup, capability resolution"
```

---

### Task 4: Startup seed + single-admin upgrade migration

**Files:**
- Modify: `internal/auth/auth.go` (add `EnsureSeed`)
- Create: `internal/auth/seed_test.go`
- Modify: composition root that runs migrations — `internal/wiring/` (locate the call site right after `store.Migrate()`; e.g. `cmd/reverb/main.go` wiring). Call `deps.Auth.EnsureSeed(ctx)` once at startup after migrate.

**Interfaces:**
- Consumes: Task 1 (`DefaultSystemRoles`), Task 3 (service + queries).
- Produces: `func (s *Service) EnsureSeed(ctx context.Context) error` — idempotent: seeds system roles + registration-policy defaults; if `users` is empty and a legacy `admin_password_hash` setting exists, creates the owner from it and back-fills null-user sessions.
- Registration-policy setting keys: `signup_enabled` (`"false"`), `invites_enabled` (`"false"`), `default_role_id` (`"role-user"`).

- [ ] **Step 1: Write failing tests**

```go
// internal/auth/seed_test.go
package auth

import (
	"context"
	"testing"
)

func TestEnsureSeedIsIdempotentAndSeedsRoles(t *testing.T) {
	s, q := newTestServiceNoSeed(t) // migrated store, NO seed yet
	ctx := context.Background()
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSeed(ctx); err != nil { // second call must be a no-op, not a UNIQUE error
		t.Fatalf("second seed failed: %v", err)
	}
	roles, _ := q.ListRoles(ctx)
	if len(roles) != 3 {
		t.Fatalf("want 3 system roles, got %d", len(roles))
	}
	if v, _ := q.GetSetting(ctx, "default_role_id"); v != "role-user" {
		t.Errorf("default_role_id = %q", v)
	}
}

func TestEnsureSeedMigratesLegacyAdmin(t *testing.T) {
	s, q := newTestServiceNoSeed(t)
	ctx := context.Background()
	h, _ := HashPassword("legacy-pw")
	_ = q.UpsertSetting(ctx, db.UpsertSettingParams{Key: "admin_password_hash", Value: h})
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	uid, err := s.Login(ctx, "admin", "legacy-pw") // existing password keeps working
	if err != nil {
		t.Fatalf("legacy login failed: %v", err)
	}
	u, _ := q.GetUserByID(ctx, uid)
	if u.IsOwner != 1 || u.RoleID != "role-admin" {
		t.Errorf("migrated admin not owner/admin: %+v", u)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/ -run TestEnsureSeed -v`
Expected: FAIL (undefined EnsureSeed).

- [ ] **Step 3: Implement**

```go
func (s *Service) EnsureSeed(ctx context.Context) error {
	// 1. system roles (idempotent — skip any that exist)
	for _, sr := range DefaultSystemRoles() {
		if _, err := s.q.GetRole(ctx, sr.ID); err == nil {
			continue
		}
		caps, _ := json.Marshal(sr.Capabilities)
		sys := int64(0)
		if sr.IsSystem {
			sys = 1
		}
		if err := s.q.CreateRole(ctx, db.CreateRoleParams{ID: sr.ID, Name: sr.Name, IsSystem: sys, Capabilities: string(caps)}); err != nil {
			return err
		}
	}
	// 2. registration-policy defaults (only if absent)
	for k, v := range map[string]string{"signup_enabled": "false", "invites_enabled": "false", "default_role_id": "role-user"} {
		if _, err := s.q.GetSetting(ctx, k); err != nil {
			if err := s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: k, Value: v}); err != nil {
				return err
			}
		}
	}
	// 3. migrate single-admin install → owner
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		if h, err := s.q.GetSetting(ctx, keyAdminHash); err == nil && h != "" {
			id := uuid.NewString()
			if err := s.q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: "admin", PasswordHash: h, RoleID: "role-admin", IsOwner: 1}); err != nil {
				return err
			}
			if err := s.q.BackfillSessionUser(ctx, sql.NullString{String: id, Valid: true}); err != nil {
				return err
			}
		}
	}
	return nil
}
```

Keep `const keyAdminHash = "admin_password_hash"`. Add `EnsureSeed(ctx)` call in the wiring composition root right after `st.Migrate()` (and surface a startup error if it fails).

- [ ] **Step 4: Run tests + full build**

Run: `go test ./internal/auth/... && go build ./...`
Expected: PASS / clean (the wiring now calls EnsureSeed).

- [ ] **Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/seed_test.go internal/wiring/
git commit -m "feat(auth): seed system roles + migrate single-admin install to owner"
```

---

## Phase 2 — Backend management APIs + gates

### Task 5: Middleware + auth handlers (setup/login/me/logout)

**Files:**
- Modify: `internal/api/middleware.go`
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/server.go` (no new routes yet; ensure `/me` stays in the protected group)
- Modify/Create: `internal/api/auth_flow_test.go`, `internal/api/middleware` coverage in existing tests

**Interfaces:**
- Consumes: Task 3 (`ResolveSession`, `CurrentUser`, `Login`, `SetupOwner`, `CreateSession`).
- Produces:
  - context accessor `func currentUser(r *http.Request) (auth.CurrentUser, bool)`
  - `func (s *Server) requireCapability(cap string) func(http.Handler) http.Handler`
  - `func (s *Server) requireAdmin(next http.Handler) http.Handler`
  - `requireAuth` now injects `CurrentUser`; the `auth_disabled` bypass is gone.
  - `issueSession(w, r, userID string)` (signature gains userID).
  - `/me` returns `{ id, username, roleId, roleName, isOwner, capabilities: []string }`.

- [ ] **Step 1: Write failing tests**

```go
// internal/api/auth_flow_test.go — adapt existing helper that builds a test Server
func TestLoginWithUsernameAndMe(t *testing.T) {
	srv := newTestServer(t) // existing helper; ensure its store is seeded (EnsureSeed)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doGET(t, srv, "/api/v1/me", tok)
	if rr.Code != 200 {
		t.Fatalf("me = %d", rr.Code)
	}
	var me struct {
		Username     string   `json:"username"`
		IsOwner      bool     `json:"isOwner"`
		Capabilities []string `json:"capabilities"`
	}
	json.Unmarshal(rr.Body.Bytes(), &me)
	if me.Username != "owner" || !me.IsOwner || !contains(me.Capabilities, "is_admin") {
		t.Fatalf("me payload wrong: %+v", me)
	}
}

func TestProtectedRouteRejectsNoSession(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	rr := doGET(t, srv, "/api/v1/me", "")
	if rr.Code != 401 {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}
```

(Use/extend the existing test helpers in `internal/api/testhelpers_test.go`; `mustSetupOwner` POSTs `/setup/admin` with `{username,password}`, `mustLogin` POSTs `/auth/login` and returns the cookie token.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestLoginWithUsernameAndMe -v`
Expected: FAIL.

- [ ] **Step 3: Implement middleware**

```go
// middleware.go
type ctxKey int
const userCtxKey ctxKey = iota

func currentUser(r *http.Request) (auth.CurrentUser, bool) {
	cu, ok := r.Context().Value(userCtxKey).(auth.CurrentUser)
	return cu, ok
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cu, err := s.deps.Auth.ResolveSession(r.Context(), s.tokenFromRequest(r))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, cu)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireCapability(cap string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cu, ok := currentUser(r)
			if !ok || !cu.Has(cap) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.requireCapability(auth.CapAdmin)(next)
}
```

Add imports `context` and `github.com/maxjb-xyz/reverb/internal/auth`. Delete the `IsAuthDisabled` bypass block.

- [ ] **Step 4: Implement handler changes**

```go
// handlers.go
type credsBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	if req, _ := s.deps.Auth.IsSetupRequired(r.Context()); !req {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "setup already complete"})
		return
	}
	var b credsBody
	if err := decode(r, &b); err != nil || b.Username == "" || b.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}
	uid, err := s.deps.Auth.SetupOwner(r.Context(), b.Username, b.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create owner"})
		return
	}
	s.issueSession(w, r, uid)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var b credsBody
	if err := decode(r, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	uid, err := s.deps.Auth.Login(r.Context(), b.Username, b.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	s.issueSession(w, r, uid)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID string) {
	tok, err := s.deps.Auth.CreateSession(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	s.setSessionCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	caps := make([]string, 0, len(cu.Caps))
	for _, c := range auth.AllCapabilities() {
		if cu.Caps[c.Key] {
			caps = append(caps, c.Key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": cu.ID, "username": cu.Username, "roleId": cu.RoleID,
		"roleName": cu.RoleName, "isOwner": cu.IsOwner, "capabilities": caps,
	})
}
```

Delete `passwordBody` if now unused. Import `auth` in handlers.go.

- [ ] **Step 5: Run tests + commit**

Run: `go test ./internal/api/ -run 'TestLogin|TestProtected|TestSetup|TestAuth' -v && go build ./...`
Expected: PASS.

```bash
git add internal/api/middleware.go internal/api/handlers.go internal/api/server.go internal/api/*_test.go
git commit -m "feat(api): user sessions in context, capability middleware, username login/me"
```

---

### Task 6: Account handlers (self-service)

**Files:**
- Create: `internal/api/account.go`, `internal/api/account_test.go`
- Modify: `internal/api/server.go` (routes), `internal/auth/auth.go` (add `ChangeOwnPassword`, `LogoutAll`)

**Interfaces:**
- Consumes: Task 5 (`currentUser`), Task 3 service.
- Produces (service): `func (s *Service) ChangeOwnPassword(ctx, userID, current, next string) error` (`ErrInvalidCreds` if current wrong); `func (s *Service) LogoutAll(ctx, userID, exceptToken string) error`.
- Routes (in protected group): `GET /account`, `POST /account/password`, `POST /account/logout-all`.

- [ ] **Step 1: Failing test**

```go
// internal/api/account_test.go
func TestChangeOwnPassword(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"pw12345","new":"newpw678"}`)
	if rr.Code != 200 {
		t.Fatalf("change pw = %d", rr.Code)
	}
	// old password no longer works; new one does
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"pw12345"}`).Code != 401 {
		t.Fatal("old password should be rejected")
	}
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"newpw678"}`).Code != 200 {
		t.Fatal("new password should work")
	}
}

func TestChangeOwnPasswordWrongCurrent(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	if doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"WRONG","new":"newpw678"}`).Code != 400 {
		t.Fatal("wrong current should 400")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestChangeOwnPassword -v`
Expected: FAIL.

- [ ] **Step 3: Implement service methods**

```go
// auth.go
func (s *Service) ChangeOwnPassword(ctx context.Context, userID, current, next string) error {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return ErrInvalidCreds
	}
	if !VerifyPassword(u.PasswordHash, current) {
		return ErrInvalidCreds
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.q.SetUserPassword(ctx, db.SetUserPasswordParams{PasswordHash: h, ID: userID})
}

func (s *Service) LogoutAll(ctx context.Context, userID, exceptToken string) error {
	return s.q.DeleteSessionsForUserExcept(ctx, db.DeleteSessionsForUserExceptParams{UserID: sql.NullString{String: userID, Valid: true}, TokenHash: hashToken(exceptToken)})
}
```

- [ ] **Step 4: Implement handlers + routes**

```go
// account.go
package api

import "net/http"

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	caps := make([]string, 0)
	for _, c := range authAllCaps() { // small local helper or inline auth.AllCapabilities()
		if cu.Caps[c.Key] {
			caps = append(caps, c.Key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": cu.ID, "username": cu.Username, "roleName": cu.RoleName,
		"isOwner": cu.IsOwner, "capabilities": caps,
	})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	var b struct{ Current, New string }
	if err := decode(r, &b); err != nil || b.New == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current and new required"})
		return
	}
	if err := s.deps.Auth.ChangeOwnPassword(r.Context(), cu.ID, b.Current, b.New); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current password incorrect"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	_ = s.deps.Auth.LogoutAll(r.Context(), cu.ID, s.tokenFromRequest(r))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

Use `auth.AllCapabilities()` directly (import auth) rather than a local helper. Wire in `server.go` protected group:

```go
pr.Get("/account", s.handleAccount)
pr.Post("/account/password", s.handleChangePassword)
pr.Post("/account/logout-all", s.handleLogoutAll)
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/api/ -run 'TestChangeOwnPassword|TestAccount' -v`
Expected: PASS.

```bash
git add internal/api/account.go internal/api/account_test.go internal/api/server.go internal/auth/auth.go
git commit -m "feat(api): account self-service (profile, change password, logout-all)"
```

---

### Task 7: Admin user-management handlers + owner guardrails

**Files:**
- Create: `internal/api/users.go`, `internal/api/users_test.go`
- Modify: `internal/api/server.go` (admin route group), `internal/auth/auth.go` (user-management service methods + errors)

**Interfaces:**
- Consumes: Tasks 3/5.
- Produces (service):
  - `type UserView struct { ID, Username, RoleID, RoleName string; IsOwner, Disabled bool; CreatedAt int64; LastSeen *int64 }`
  - `func (s *Service) ListUsers(ctx) ([]UserView, error)`
  - `func (s *Service) CreateUser(ctx, username, password, roleID string) (string, error)` (`ErrUsernameTaken`, role-must-exist)
  - `func (s *Service) UpdateUserRole(ctx, id, roleID string) error` (`ErrOwnerProtected` if target is owner and new role != admin)
  - `func (s *Service) SetUserDisabled(ctx, id string, disabled bool) error` (`ErrOwnerProtected` if owner)
  - `func (s *Service) AdminSetPassword(ctx, id, password string) error`
  - `func (s *Service) DeleteUser(ctx, id string) error` (`ErrOwnerProtected` if owner)
  - Errors: `ErrUsernameTaken`, `ErrOwnerProtected`, `ErrRoleNotFound`.
- Routes (admin group, `requireCapability(can_manage_users)`): `GET/POST /users`, `PATCH/DELETE /users/{id}`, `POST /users/{id}/password`.

- [ ] **Step 1: Failing test (owner guardrail is the load-bearing case)**

```go
// internal/api/users_test.go
func TestAdminCreatesUserAndOwnerProtected(t *testing.T) {
	srv := newTestServer(t)
	ownerID := mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")

	// create a regular user
	rr := doPOST(t, srv, "/api/v1/users", tok, `{"username":"bob","password":"bobpw123","roleId":"role-user"}`)
	if rr.Code != 201 {
		t.Fatalf("create user = %d (%s)", rr.Code, rr.Body)
	}
	// bob can log in
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"bob","password":"bobpw123"}`).Code != 200 {
		t.Fatal("bob should be able to log in")
	}
	// owner cannot be deleted
	if doDELETE(t, srv, "/api/v1/users/"+ownerID, tok).Code != 409 {
		t.Fatal("owner delete must 409")
	}
	// owner cannot be demoted
	if doPATCH(t, srv, "/api/v1/users/"+ownerID, tok, `{"roleId":"role-user"}`).Code != 409 {
		t.Fatal("owner demotion must 409")
	}
}

func TestNonAdminCannotManageUsers(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	otok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", otok, `{"username":"req","password":"reqpw123","roleId":"role-requester"}`)
	rtok := mustLogin(t, srv, "req", "reqpw123")
	if doGET(t, srv, "/api/v1/users", rtok).Code != 403 {
		t.Fatal("requester must be forbidden from listing users")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestAdminCreatesUser|TestNonAdmin' -v`
Expected: FAIL.

- [ ] **Step 3: Implement service methods**

```go
// auth.go
var (
	ErrUsernameTaken  = errors.New("username taken")
	ErrOwnerProtected = errors.New("owner account is protected")
	ErrRoleNotFound   = errors.New("role not found")
)

func (s *Service) CreateUser(ctx context.Context, username, password, roleID string) (string, error) {
	if _, err := s.q.GetRole(ctx, roleID); err != nil {
		return "", ErrRoleNotFound
	}
	if _, err := s.q.GetUserByUsername(ctx, username); err == nil {
		return "", ErrUsernameTaken
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	return id, s.q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: username, PasswordHash: h, RoleID: roleID, IsOwner: 0})
}

func (s *Service) UpdateUserRole(ctx context.Context, id, roleID string) error {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return ErrRoleNotFound
	}
	if u.IsOwner == 1 && roleID != "role-admin" {
		return ErrOwnerProtected
	}
	if _, err := s.q.GetRole(ctx, roleID); err != nil {
		return ErrRoleNotFound
	}
	return s.q.UpdateUserRole(ctx, db.UpdateUserRoleParams{RoleID: roleID, ID: id})
}

func (s *Service) DeleteUser(ctx context.Context, id string) error {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return nil
	}
	if u.IsOwner == 1 {
		return ErrOwnerProtected
	}
	return s.q.DeleteUser(ctx, id)
}
```

`SetUserDisabled` mirrors the owner check; `AdminSetPassword` hashes + `SetUserPassword`; `ListUsers` maps rows → `UserView` (resolving `RoleName` via `GetRole`, converting null `last_seen`).

- [ ] **Step 4: Implement handlers + routes**

`users.go`: `handleListUsers`, `handleCreateUser` (decode `{username,password,roleId}`; map `ErrUsernameTaken`→409, `ErrRoleNotFound`→400; success 201), `handleUpdateUser` (decode `{roleId?,disabled?}`; map `ErrOwnerProtected`→409), `handleDeleteUser` (`ErrOwnerProtected`→409, else 200), `handleAdminResetPassword` (decode `{password}`→200). Read `chi.URLParam(r,"id")` for the id.

Wire in `server.go` — add an admin sub-group inside the protected group:

```go
pr.Group(func(ar chi.Router) {
	ar.Use(s.requireCapability(auth.CapManageUsers))
	ar.Get("/users", s.handleListUsers)
	ar.Post("/users", s.handleCreateUser)
	ar.Patch("/users/{id}", s.handleUpdateUser)
	ar.Delete("/users/{id}", s.handleDeleteUser)
	ar.Post("/users/{id}/password", s.handleAdminResetPassword)
})
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/api/ -run 'TestAdminCreatesUser|TestNonAdmin' -v`
Expected: PASS.

```bash
git add internal/api/users.go internal/api/users_test.go internal/api/server.go internal/auth/auth.go
git commit -m "feat(api): admin user management with owner guardrails"
```

---

### Task 8: Role-management handlers + system-role protection + capabilities metadata

**Files:**
- Create: `internal/api/roles.go`, `internal/api/roles_test.go`
- Modify: `internal/api/server.go`, `internal/auth/auth.go`

**Interfaces:**
- Produces (service):
  - `type RoleView struct { ID, Name string; IsSystem bool; Capabilities []string }`
  - `func (s *Service) ListRoles(ctx) ([]RoleView, error)`
  - `func (s *Service) CreateRole(ctx, name string, caps []string) (string, error)` (`ValidateCapabilities`; `ErrInvalidCapability`)
  - `func (s *Service) UpdateRole(ctx, id, name string, caps []string) error` (`ErrSystemRole` if `is_system`; validate caps)
  - `func (s *Service) DeleteRole(ctx, id string) error` (`ErrSystemRole`; `ErrRoleInUse` if `CountUsersWithRole>0`)
  - Errors: `ErrSystemRole`, `ErrRoleInUse`.
- Routes (admin `requireAdmin`): `GET/POST /roles`, `PATCH/DELETE /roles/{id}`, `GET /capabilities`.

- [ ] **Step 1: Failing test**

```go
func TestRolesCrudAndProtection(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")

	// create custom role
	rr := doPOST(t, srv, "/api/v1/roles", tok, `{"name":"DJ","capabilities":["can_create_playlists","can_download"]}`)
	if rr.Code != 201 {
		t.Fatalf("create role = %d (%s)", rr.Code, rr.Body)
	}
	// invalid capability rejected
	if doPOST(t, srv, "/api/v1/roles", tok, `{"name":"Bad","capabilities":["can_teleport"]}`).Code != 400 {
		t.Fatal("invalid cap must 400")
	}
	// system role capabilities are read-only
	if doPATCH(t, srv, "/api/v1/roles/role-admin", tok, `{"name":"Admin","capabilities":["can_request"]}`).Code != 409 {
		t.Fatal("editing system role must 409")
	}
	// system role undeletable
	if doDELETE(t, srv, "/api/v1/roles/role-user", tok).Code != 409 {
		t.Fatal("deleting system role must 409")
	}
}

func TestCapabilitiesMetadata(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doGET(t, srv, "/api/v1/capabilities", tok)
	var caps []struct{ Key, Label string }
	json.Unmarshal(rr.Body.Bytes(), &caps)
	if len(caps) != 6 {
		t.Fatalf("want 6 capabilities, got %d", len(caps))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestRolesCrud|TestCapabilitiesMetadata' -v`
Expected: FAIL.

- [ ] **Step 3: Implement service methods**

```go
var (
	ErrSystemRole = errors.New("system role is protected")
	ErrRoleInUse  = errors.New("role is assigned to users")
)

func (s *Service) CreateRole(ctx context.Context, name string, caps []string) (string, error) {
	if err := ValidateCapabilities(caps); err != nil {
		return "", err
	}
	b, _ := json.Marshal(caps)
	id := "role-" + uuid.NewString()
	return id, s.q.CreateRole(ctx, db.CreateRoleParams{ID: id, Name: name, IsSystem: 0, Capabilities: string(b)})
}

func (s *Service) UpdateRole(ctx context.Context, id, name string, caps []string) error {
	r, err := s.q.GetRole(ctx, id)
	if err != nil {
		return ErrRoleNotFound
	}
	if r.IsSystem == 1 {
		return ErrSystemRole
	}
	if err := ValidateCapabilities(caps); err != nil {
		return err
	}
	b, _ := json.Marshal(caps)
	return s.q.UpdateRole(ctx, db.UpdateRoleParams{Name: name, Capabilities: string(b), ID: id})
}

func (s *Service) DeleteRole(ctx context.Context, id string) error {
	r, err := s.q.GetRole(ctx, id)
	if err != nil {
		return nil
	}
	if r.IsSystem == 1 {
		return ErrSystemRole
	}
	if n, _ := s.q.CountUsersWithRole(ctx, id); n > 0 {
		return ErrRoleInUse
	}
	return s.q.DeleteRole(ctx, id)
}
```

- [ ] **Step 4: Handlers + routes**

`roles.go`: `handleListRoles`, `handleCreateRole` (201; `ErrInvalidCapability`→400), `handleUpdateRole` (`ErrSystemRole`→409, `ErrInvalidCapability`→400), `handleDeleteRole` (`ErrSystemRole`/`ErrRoleInUse`→409), `handleCapabilities` (returns `auth.AllCapabilities()`). Wire into the admin group:

```go
ar.Get("/roles", s.handleListRoles)
ar.Post("/roles", s.handleCreateRole)
ar.Patch("/roles/{id}", s.handleUpdateRole)
ar.Delete("/roles/{id}", s.handleDeleteRole)
ar.Get("/capabilities", s.handleCapabilities)
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/api/ -run 'TestRolesCrud|TestCapabilities' -v`
Expected: PASS.

```bash
git add internal/api/roles.go internal/api/roles_test.go internal/api/server.go internal/auth/auth.go
git commit -m "feat(api): role management, system-role protection, capability metadata"
```

---

### Task 9: Signup, invites, registration policy

**Files:**
- Create: `internal/api/registration.go`, `internal/api/registration_test.go`
- Modify: `internal/api/server.go` (public `/auth/signup`; admin invites + policy), `internal/auth/auth.go`

**Interfaces:**
- Produces (service):
  - `type RegPolicy struct { SignupEnabled, InvitesEnabled bool; DefaultRoleID string }`
  - `func (s *Service) GetRegPolicy(ctx) (RegPolicy, error)` / `func (s *Service) SetRegPolicy(ctx, RegPolicy) error`
  - `func (s *Service) Signup(ctx, username, password, inviteCode string) (string, error)` — honors policy; `ErrSignupDisabled`, `ErrInviteInvalid`, `ErrUsernameTaken`.
  - `func (s *Service) CreateInvite(ctx, roleID *string, expiresAt *int64, createdBy string) (code string, err error)`
  - `func (s *Service) ListInvites(ctx) ([]InviteView, error)` / `func (s *Service) DeleteInvite(ctx, id string) error`
- Routes: public `POST /auth/signup`; admin `GET/PATCH /settings/registration`, `GET/POST /invites`, `DELETE /invites/{id}`.

- [ ] **Step 1: Failing test**

```go
func TestSignupGatedByPolicy(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	// signup disabled by default
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"carol","password":"carolpw1"}`).Code != 403 {
		t.Fatal("signup should be disabled by default")
	}
	// admin enables open signup
	tok := mustLogin(t, srv, "owner", "pw12345")
	if doPATCH(t, srv, "/api/v1/settings/registration", tok, `{"signupEnabled":true,"invitesEnabled":false,"defaultRoleId":"role-user"}`).Code != 200 {
		t.Fatal("policy update failed")
	}
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"carol","password":"carolpw1"}`).Code != 200 {
		t.Fatal("signup should now succeed")
	}
	// carol got the default role
	ctok := mustLogin(t, srv, "carol", "carolpw1")
	rr := doGET(t, srv, "/api/v1/me", ctok)
	if !bytesContain(rr.Body.Bytes(), `"roleId":"role-user"`) {
		t.Fatalf("carol role wrong: %s", rr.Body)
	}
}

func TestInviteRedemptionAssignsRole(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	doPATCH(t, srv, "/api/v1/settings/registration", tok, `{"signupEnabled":false,"invitesEnabled":true,"defaultRoleId":"role-user"}`)
	rr := doPOST(t, srv, "/api/v1/invites", tok, `{"roleId":"role-requester"}`)
	var inv struct{ Code string }
	json.Unmarshal(rr.Body.Bytes(), &inv)
	// signup with invite works and assigns the invite's role
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"dave","password":"davepw12","invite":"`+inv.Code+`"}`).Code != 200 {
		t.Fatal("invite signup should succeed")
	}
	// invite cannot be reused
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"erin","password":"erinpw12","invite":"`+inv.Code+`"}`).Code != 403 {
		t.Fatal("used invite must be rejected")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestSignup|TestInvite' -v`
Expected: FAIL.

- [ ] **Step 3: Implement service**

```go
var (
	ErrSignupDisabled = errors.New("signup disabled")
	ErrInviteInvalid  = errors.New("invite invalid")
)

func (s *Service) Signup(ctx context.Context, username, password, inviteCode string) (string, error) {
	pol, err := s.GetRegPolicy(ctx)
	if err != nil {
		return "", err
	}
	roleID := pol.DefaultRoleID
	var inviteID string
	if inviteCode != "" {
		if !pol.InvitesEnabled {
			return "", ErrInviteInvalid
		}
		inv, err := s.q.GetInviteByCode(ctx, inviteCode)
		if err != nil || inv.UsedAt.Valid || (inv.ExpiresAt.Valid && inv.ExpiresAt.Int64 < s.now().Unix()) {
			return "", ErrInviteInvalid
		}
		inviteID = inv.ID
		if inv.RoleID.Valid {
			roleID = inv.RoleID.String
		}
	} else if !pol.SignupEnabled {
		return "", ErrSignupDisabled
	}
	if _, err := s.q.GetUserByUsername(ctx, username); err == nil {
		return "", ErrUsernameTaken
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	if err := s.q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: username, PasswordHash: h, RoleID: roleID, IsOwner: 0}); err != nil {
		return "", err
	}
	if inviteID != "" {
		_ = s.q.MarkInviteUsed(ctx, db.MarkInviteUsedParams{UsedBy: sql.NullString{String: id, Valid: true}, ID: inviteID})
	}
	return id, nil
}
```

`GetRegPolicy`/`SetRegPolicy` read/write the three settings keys (parse `"true"`/`"false"`). `CreateInvite` generates a short random code (e.g. 16 url-safe bytes), inserts with nullable role/expiry. `ListInvites`/`DeleteInvite` map rows.

- [ ] **Step 4: Handlers + routes**

`registration.go`: `handleSignup` (public; map `ErrSignupDisabled`/`ErrInviteInvalid`→403, `ErrUsernameTaken`→409; on success issue a session for the new user via `s.issueSession(w, r, uid)`), `handleGetRegistration`/`handlePatchRegistration` (admin), `handleListInvites`/`handleCreateInvite` (returns `{code}`; 201)/`handleDeleteInvite`. Wire:

```go
// public group:
r.Post("/auth/signup", s.handleSignup)
// admin group:
ar.Get("/settings/registration", s.handleGetRegistration)
ar.Patch("/settings/registration", s.handlePatchRegistration)
ar.Get("/invites", s.handleListInvites)
ar.Post("/invites", s.handleCreateInvite)
ar.Delete("/invites/{id}", s.handleDeleteInvite)
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/api/ -run 'TestSignup|TestInvite' -v`
Expected: PASS.

```bash
git add internal/api/registration.go internal/api/registration_test.go internal/api/server.go internal/auth/auth.go
git commit -m "feat(api): configurable signup, invites, registration policy"
```

---

### Task 10: Apply capability gates + ownership to existing routes

**Files:**
- Modify: `internal/api/server.go` (gate existing groups)
- Modify: `internal/api/downloads.go` (initiated_by attribution + `can_download` gate verified)
- Modify: `internal/api/synced_playlists.go` (owner scoping) + `internal/download/manager.go` if `Enqueue` needs the user id threaded
- Modify: `internal/core/types.go` (add `InitiatedBy` to `DownloadRequest` if not present)
- Test: `internal/api/library_test.go` / `downloads_test.go` / `synced_playlists_test.go` (gate + ownership cases)

**Interfaces:**
- Consumes: Task 5 middleware, `currentUser`.
- Behavior: adapter/settings/library-config routes require `can_manage_library`; download create/batch require `can_download` (403 otherwise) and stamp `initiated_by`; playlist create/mutate require `can_create_playlists`; playlist read/write enforce `owner_user_id == currentUser.id` (admin bypass) and the list returns only the caller's playlists. EnsureSeed/migration assigns existing playlists' `owner_user_id` to the owner (extend Task-4 EnsureSeed: after creating/identifying the owner, `UPDATE synced_playlists SET owner_user_id = <owner> WHERE owner_user_id IS NULL`).

- [ ] **Step 1: Failing tests**

```go
func TestDownloadRequiresCapability(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	otok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", otok, `{"username":"req","password":"reqpw123","roleId":"role-requester"}`)
	rtok := mustLogin(t, srv, "req", "reqpw123")
	// requester lacks can_download
	if doPOST(t, srv, "/api/v1/downloads", rtok, `{"source":"spotify","externalId":"x","title":"t","artist":"a"}`).Code != 403 {
		t.Fatal("requester download must 403")
	}
}

func TestPlaylistsScopedToOwner(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	otok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", otok, `{"username":"bob","password":"bobpw123","roleId":"role-user"}`)
	btok := mustLogin(t, srv, "bob", "bobpw123")
	// bob creates a playlist
	doPOST(t, srv, "/api/v1/playlists", btok, `{"name":"Bobs Mix"}`)
	// owner's list does not include bob's playlist
	rr := doGET(t, srv, "/api/v1/playlists", otok)
	if bytesContain(rr.Body.Bytes(), "Bobs Mix") {
		t.Fatal("playlists must be scoped to owner")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestDownloadRequiresCapability|TestPlaylistsScopedToOwner' -v`
Expected: FAIL.

- [ ] **Step 3: Gate routes in server.go**

Wrap the relevant route registrations with capability middleware. Library/integration config:

```go
pr.Group(func(mr chi.Router) {
	mr.Use(s.requireCapability(auth.CapManageLibrary))
	mr.Get("/adapters/available", s.handleAdaptersAvailable)
	mr.Get("/adapters", s.handleListAdapters)
	mr.Post("/adapters", s.handleCreateAdapter)
	mr.Put("/adapters/{id}", s.handleUpdateAdapter)
	mr.Delete("/adapters/{id}", s.handleDeleteAdapter)
	mr.Post("/adapters/test", s.handleTestAdapter)
	mr.Get("/settings", s.handleGetSettings)
	mr.Put("/settings", s.handlePutSettings)
})
```

Download create/batch → `s.requireCapability(auth.CapDownload)`; playlist create/import/mutate → `s.requireCapability(auth.CapCreatePlaylists)`. (Reads like `GET /library/*`, `/stream`, `/cover`, `/search/*`, playlist *reads* stay only `requireAuth`.)

- [ ] **Step 4: Thread ownership + attribution**

- In the playlist create/list/detail/mutation handlers, read `cu, _ := currentUser(r)` and pass `cu.ID` to the synced-playlist store calls; add an `owner_user_id` column predicate to the list/detail/mutation queries (new sqlc query params — e.g. `ListSyncedPlaylistsForOwner`, and an ownership check in detail/mutation that 404s/403s when `owner_user_id != cu.ID` and not admin). Update `synced_playlists.sql` + `make gen`.
- In `handleCreateDownload`/`handleBatchDownload`, set `req.InitiatedBy = cu.ID` before `Enqueue`; thread it into the job store insert (`download_jobs.initiated_by`). Add `InitiatedBy string` to `core.DownloadRequest` and persist it in `internal/download/sqlstore.go`.
- Extend Task-4 `EnsureSeed` owner branch with the playlist back-fill `UPDATE`.

- [ ] **Step 5: Run gate + full suite + commit**

Run: `go test ./... && go build ./... && go vet ./...`
Expected: PASS. (Fix any now-unauthorized existing tests by seeding an owner + using an authed admin token via the shared helper.)

```bash
git add -A
git commit -m "feat(api): capability gates + playlist ownership + download attribution"
```

---

## Phase 3 — Frontend

> All FE tasks: design tokens only; match existing component idioms; run `npx vitest run` for the touched files and keep `npx tsc --noEmit` clean. Commit after each task.

### Task 11: Auth/session store — current user + capabilities

**Files:**
- Modify: `web/src/lib/session.ts`, `web/src/lib/session.test.ts`
- Create: `web/src/lib/authStore.ts`, `web/src/lib/authStore.test.ts`

**Interfaces:**
- Produces:
  - `type Me = { id: string; username: string; roleId: string; roleName: string; isOwner: boolean; capabilities: string[] }`
  - `fetchMe(): Promise<Me | null>` (GET `/api/v1/me`; null on 401)
  - Zustand `useAuthStore` with `{ me: Me | null; loading: boolean; refresh(): Promise<void>; can(cap: string): boolean; logout(): Promise<void> }`
  - `login(username, password)`, `signup(username, password, invite?)`, `setupOwner(username, password)` in `session.ts` (POST the username+password bodies).

- [ ] **Step 1: Failing test**

```ts
// web/src/lib/authStore.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useAuthStore } from './authStore'

beforeEach(() => {
  useAuthStore.setState({ me: null, loading: false })
})

it('can() reflects capabilities from /me', async () => {
  vi.spyOn(global, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    id: 'u1', username: 'owner', roleId: 'role-admin', roleName: 'Admin', isOwner: true,
    capabilities: ['is_admin', 'can_download'],
  }), { status: 200 }))
  await useAuthStore.getState().refresh()
  expect(useAuthStore.getState().can('is_admin')).toBe(true)
  expect(useAuthStore.getState().can('can_request')).toBe(false)
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/lib/authStore.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement** the `fetchMe`, `useAuthStore` (refresh sets me/loading; `can` reads `me?.capabilities.includes(cap) ?? false`; logout POSTs `/api/v1/auth/logout` then clears me), and the login/signup/setup POST helpers in `session.ts`, following the existing fetch wrapper in `web/src/lib/api.ts`.

- [ ] **Step 4: Run test**

Run: `cd web && npx vitest run src/lib/authStore.test.ts && npx tsc --noEmit`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/authStore.ts web/src/lib/authStore.test.ts web/src/lib/session.ts web/src/lib/session.test.ts
git commit -m "feat(web): auth store with current user + capability checks"
```

---

### Task 12: Login + first-run setup pages (username)

**Files:**
- Modify: `web/src/routes/Login.tsx`, `web/src/routes/Setup.tsx`, `web/src/routes/Setup.test.tsx`

**Interfaces:**
- Consumes: Task 11 (`login`, `setupOwner`, `useAuthStore.refresh`).

- [ ] **Step 1: Failing test** — extend `Setup.test.tsx` to fill a **username** + password and assert `setupOwner` is called with both; add the analogous Login assertion.
- [ ] **Step 2: Run to verify failure** — `cd web && npx vitest run src/routes/Setup.test.tsx` → FAIL.
- [ ] **Step 3: Implement** — add a username `<input>` to both forms (token-styled, matching existing field markup); submit `{username, password}`; on success call `useAuthStore.getState().refresh()` and navigate home.
- [ ] **Step 4: Run** — `cd web && npx vitest run src/routes/Setup.test.tsx src/routes/Login.tsx && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(web): username on login + first-run setup"`.

---

### Task 13: Signup / invite-redeem page

**Files:**
- Create: `web/src/routes/Signup.tsx`, `web/src/routes/Signup.test.tsx`
- Modify: `web/src/App.tsx` (route `/signup`), `web/src/routes/Login.tsx` (link to signup when allowed)

**Interfaces:**
- Consumes: Task 11 (`signup`), a public GET that reports whether signup/invites are open. Add `GET /api/v1/auth/registration-status` (public; returns `{signupEnabled, invitesEnabled}`) in `internal/api/registration.go` + a public route, so the page can show/hide itself. (Small addition — include in this task; add a Go test asserting the public endpoint shape.)

- [ ] **Step 1: Failing test** — render `Signup`, fill username/password (and read `?invite=CODE` from the URL into a hidden field), submit, assert `signup` called with the invite code.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** the page (token-styled, mirrors Login), the `?invite=` prefill, the public registration-status fetch + Go endpoint, and the conditional "Create account" link on Login.
- [ ] **Step 4: Run** `cd web && npx vitest run src/routes/Signup.test.tsx && npx tsc --noEmit` and `go test ./internal/api/ -run TestRegistrationStatus` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(web): signup + invite-redeem page"`.

---

### Task 14: Account page

**Files:**
- Create: `web/src/routes/Account.tsx`, `web/src/routes/Account.test.tsx`, `web/src/lib/accountApi.ts`
- Modify: `web/src/App.tsx` (route `/account`), the user menu in `web/src/components/AppShell.tsx` (link to Account + logout)

**Interfaces:**
- Consumes: Task 11 store; `accountApi`: `changePassword(current, next)`, `logoutAll()`.

- [ ] **Step 1: Failing test** — render `Account`; assert it shows `me.username`, `me.roleName`, capability chips; fill change-password form (new ≠ confirm shows inline error; matching submits and calls `changePassword`).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** the page: Profile (username, role badge, member-since, capability chips read-only), Security (change password with current/new/confirm + inline validation), Sessions (sign out, sign out everywhere). Token styling, matches Settings page density.
- [ ] **Step 4: Run** `cd web && npx vitest run src/routes/Account.test.tsx && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(web): account page (profile, change password, sessions)"`.

---

### Task 15: Admin Users page (table + create/edit)

**Files:**
- Create: `web/src/routes/Users.tsx`, `web/src/routes/Users.test.tsx`, `web/src/lib/usersApi.ts`
- Modify: `web/src/App.tsx` (route `/settings/users`, admin-guarded), navigation entry in `AppShell.tsx`/`Settings.tsx` (shown only when `can('can_manage_users')`)

**Interfaces:**
- Consumes: Task 11 (`can`), `usersApi`: `listUsers`, `createUser`, `updateUser`, `deleteUser`, `resetPassword`, `listRoles`.

- [ ] **Step 1: Failing test** — mock `listUsers` → owner + bob; render `Users`; assert the table renders both, the owner row shows a lock and no delete button, and "Create user" opens a modal that posts username/password/role.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** the table (username, role, status, created, last-seen), row actions (edit role via a select that calls `updateUser`, reset password, disable/enable, delete — all hidden/disabled for the owner row with a lock + tooltip), and the Create-user modal. Token-styled, matches existing table/modal idioms.
- [ ] **Step 4: Run** `cd web && npx vitest run src/routes/Users.test.tsx && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(web): admin users management page"`.

---

### Task 16: Roles editor + registration policy + invites UI

**Files:**
- Modify: `web/src/routes/Users.tsx` (Roles panel + Registration card + Invites section), `web/src/routes/Users.test.tsx`, `web/src/lib/usersApi.ts` (roles/invites/policy calls)

**Interfaces:**
- Consumes: `usersApi`: `listRoles`, `createRole`, `updateRole`, `deleteRole`, `getCapabilities`, `getRegistration`, `setRegistration`, `listInvites`, `createInvite`, `deleteInvite`.

- [ ] **Step 1: Failing test** — mock `getCapabilities` (6 caps) + `listRoles`; render the Roles panel; assert system roles render read-only (no editable checkboxes), a custom role's capability checklist toggles call `updateRole`, and the Registration card toggles call `setRegistration`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**: Roles panel (list with capability chips; create/edit via a capability checklist built from `getCapabilities`; system roles read-only); Registration card (signup/invites toggles + default-role select → `setRegistration`); Invites section (generate with role+expiry → copyable link; list active/used with revoke), shown when invites enabled. Token styling.
- [ ] **Step 4: Run** `cd web && npx vitest run src/routes/Users.test.tsx && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(web): role editor, registration policy, invites UI"`.

---

### Task 17: App-wide capability gating

**Files:**
- Modify: `web/src/App.tsx` (route guards: redirect unauth → login; gate `/settings/*`, `/settings/users` by capability), `web/src/components/AppShell.tsx` (hide nav entries by capability), `web/src/components/download/DownloadAction.tsx` + `DownloadPopover.tsx` (honor `can('can_download')` — hide/disable download affordance for users without it; the "Request" branch is SP2), relevant tests.

**Interfaces:**
- Consumes: Task 11 `useAuthStore`.

- [ ] **Step 1: Failing test** — render `AppShell` with a `me` lacking `is_admin`/`can_manage_library`; assert Settings/Users nav entries are absent; render with `can_download=false` and assert the download button is not shown.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** the guards + conditional nav + download affordance gating. App boots by calling `useAuthStore.refresh()`; while `loading`, render nothing/spinner; if `me == null` and not on a public route, redirect to `/login` (or `/setup` when setup is required).
- [ ] **Step 4: Run** `cd web && npx vitest run && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(web): capability-gated routes, nav, and download affordance"`.

---

### Task 18: End-to-end flow + full gate

**Files:**
- Create: `web/e2e/multiuser.spec.ts`
- Modify: any existing e2e helper that logs in (now needs a username)

**Interfaces:** Playwright against the built app.

- [ ] **Step 1: Write the e2e**

Flow: first-run setup creates owner (`owner`/`pw12345`) → lands in app → open Settings → Users → create user `bob`/`bobpw123` with role User → log out → log in as `bob` → assert Settings/Users nav entry is absent for bob → log out → log back in as owner. Keep assertions resilient to copy (use roles/test-ids).

- [ ] **Step 2: Run to verify it drives the app** — `cd web && npm run build && npm run e2e` → the new spec passes; existing specs stay green (fix the shared login helper to supply a username).

- [ ] **Step 3: Run the entire gate**

Run (root): `go test ./... && go build ./... && go vet ./...`
Run (web): `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`
Expected: all green; e2e count = prior + 1.

- [ ] **Step 4: Commit**

```bash
git add web/e2e/multiuser.spec.ts web/e2e/
git commit -m "test(e2e): multi-user setup → admin creates user → second user logs in"
```

- [ ] **Step 5: Final whole-branch review + merge**

Run `superpowers:requesting-code-review` over the branch diff; address findings; then fast-forward merge `feat/multiuser-foundation` → local `main`. Tell the user to push + rebuild the Docker image to verify on `soulkiller:8090` (do not `git push` without explicit go-ahead).

---

## Self-Review

**Spec coverage check (spec §→task):**
- §3.1 capabilities/roles/users model → Tasks 1, 2, 3 ✓
- §3.2 current-user invariant / middleware → Task 5 ✓
- §3.3 schema (users/roles/invites + session/download/playlist columns) → Task 2 ✓
- §3.4 seed + single-admin migration + `IsSetupRequired` by count → Tasks 3, 4 ✓
- §4 authorization gates + ownership (downloads 403, playlist scoping, library admin-gate, owner guardrail) → Tasks 5, 7, 10 ✓
- §5 API surface — setup/login/me/logout (5), account (6), users (7), roles+capabilities (8), invites/policy/signup (9) ✓
- §6 FE — auth store (11), login/setup (12), signup (13), account page (14), admin users (15), roles/policy/invites (16), app gating (17) ✓
- §7 backend shape (capabilities.go, auth.go, queries, middleware, handlers, wiring) → Tasks 1–10 ✓
- §8 testing (migration, auth flows, authz 403s, ownership, role protection, owner guardrail, account, FE/e2e) → distributed; e2e in 18 ✓
- §9 rollout/migration → Task 4 + Task 18 merge step ✓
- §10 deferred items — intentionally NOT built (request workflow, history, sharing, OIDC, user_settings) ✓ no tasks, correct.

**Placeholder scan:** No "TBD/TODO/handle edge cases"; each validation/error-mapping is named explicitly (status codes given per handler). Sibling CRUD handlers specify exact routes, bodies, and status mappings rather than vague "implement similarly."

**Type consistency:** `CurrentUser{ID,Username,RoleID,RoleName,IsOwner,Caps}` used identically in Tasks 3/5/6/7. Capability keys + role IDs are fixed in Global Constraints and reused verbatim. `credsBody{Username,Password}` (5) reused by 6/9 flows. `Me` FE type (11) matches `/me` JSON (5). Service signatures in each task's Interfaces block match their call sites in later tasks.

**Note on Task 2 gate independence:** the `CreateSession` signature change transiently breaks callers; the plan flags resolving Tasks 2+3 together if a clean per-task gate is required. Reviewer may accept the merged commit.
