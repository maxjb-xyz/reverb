# Permissions Model Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-frame Reverb's permission model the Seerr way — one acquisition action (`request`) with an `auto_approve` trust modifier (no more "download"), separate the acquisition axis from playlists, make the default roles editable behind a single anti-lockout invariant, and document every capability.

**Architecture:** Rename two capability keys in the code-defined registry and add descriptions; redefine the three seeded roles; remap existing installs' role JSON idempotently in `EnsureSeed`; relax role-edit rules (drop the frozen-`is_system` treatment) and add an "always ≥1 administrator" invariant enforced at the four admin-reducing mutations; update the admin UI to editable role cards with capability help text and the renamed labels.

**Tech Stack:** Go (auth.Service, sqlc queries, chi), React 19 + TS (Zustand authStore, TanStack Query, Tailwind tokens, Playwright).

## Global Constraints

- **Capability keys after this change (exact):** `is_admin`, `can_manage_users`, `can_manage_library`, `request`, `auto_approve`, `can_create_playlists`. ONLY two keys change: `can_download`→`auto_approve` and `can_request`→`request`. The other three keep their `can_*` key strings (keys are internal; the user-facing **labels** change). Do NOT rename `can_manage_users`/`can_manage_library`/`can_create_playlists`.
- **Capability labels + descriptions (exact, user-facing):**
  - `is_admin` — "Full administrator" / "Complete access; bypasses all restrictions. Opens the Admin area."
  - `can_manage_users` — "Manage users & roles" / "Create and edit users, edit roles, and control registration & invites. Opens the Admin area."
  - `can_manage_library` — "Manage library & integrations" / "Configure the music backend, search providers, and downloaders. Opens the Admin area."
  - `request` — "Request music" / "Ask to add music to the library. Fulfilled instantly if Auto-approve is also granted; otherwise it waits for an administrator's approval."
  - `auto_approve` — "Auto-approve music" / "Requests to add music are fulfilled immediately, without approval (one-click add). Implies Request."
  - `can_create_playlists` — "Create & edit playlists" / "Make and manage their own playlists."
- **Default roles (exact):** Admin = `[is_admin, can_manage_users, can_manage_library, request, auto_approve, can_create_playlists]`; User = `[auto_approve, request, can_create_playlists]`; Requester = `[request, can_create_playlists]`. Fixed IDs unchanged: `role-admin`, `role-user`, `role-requester`.
- **Normalization:** `auto_approve` implies `request` — on create/update of any role, if caps contain `auto_approve` but not `request`, append `request`.
- **Anti-lockout invariant:** the system must always have ≥1 ENABLED user with `is_admin`. Enforced on: role-capability edit, user-role change, user delete, user disable.
- **Migration is an idempotent `EnsureSeed` remap** (no new goose file — this is a data-only fix with no schema change): remap old keys in every role's JSON and ensure `role-requester` has `can_create_playlists`. Safe to run every startup.
- **No `is_system` read-only treatment** — all roles editable/renamable; `is_system` stays only as a "Default" badge. Deletion guarded by in-use OR registration-default, not by `is_system`.
- **Generated code:** sqlc files in `internal/store/db/` stay generated (no schema change here, so no `make gen` needed). Design tokens only in FE (no raw hex / `text-black`/`text-white`; `text-error`/`text-success`).
- **Commit footer:** end every commit with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Gate:** root `go test ./... && go build ./... && go vet ./...`; `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.

---

### Task 1: Capability registry — rename, descriptions, default roles

**Files:**
- Modify: `internal/auth/capabilities.go`
- Modify: `internal/api/server.go:263` (the download-create gate)
- Modify: `internal/auth/capabilities_test.go`

**Interfaces:**
- Produces: `Capability { Key, Label, Description string }`; const `CapAutoApprove = "auto_approve"` (replaces `CapDownload`); `CapRequest = "request"` (value changed); `AllCapabilities()` returns the 6 with labels+descriptions; `DefaultSystemRoles()` per Global Constraints.
- Consumes: nothing (leaf).

- [ ] **Step 1: Update the test first**

Replace the body of `capabilities_test.go` to match the new registry:

```go
package auth

import "testing"

func TestAllCapabilitiesContainsKnownKeys(t *testing.T) {
	caps := AllCapabilities()
	if len(caps) != 6 {
		t.Fatalf("want 6 capabilities, got %d", len(caps))
	}
	want := map[string]bool{CapAdmin: false, CapManageUsers: false, CapManageLibrary: false, CapAutoApprove: false, CapRequest: false, CapCreatePlaylists: false}
	for _, c := range caps {
		if _, ok := want[c.Key]; !ok {
			t.Errorf("unexpected capability %q", c.Key)
		}
		if c.Label == "" || c.Description == "" {
			t.Errorf("capability %q missing label or description", c.Key)
		}
		want[c.Key] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing capability %q", k)
		}
	}
	// the two renamed keys are present, the old keys are gone
	if !IsCapability("auto_approve") || !IsCapability("request") {
		t.Error("renamed keys missing")
	}
	if IsCapability("can_download") || IsCapability("can_request") {
		t.Error("old keys should be gone")
	}
}

func TestValidateCapabilities(t *testing.T) {
	if err := ValidateCapabilities([]string{CapAutoApprove, CapRequest}); err != nil {
		t.Fatalf("valid caps rejected: %v", err)
	}
	if err := ValidateCapabilities([]string{"can_teleport"}); err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestDefaultSystemRoles(t *testing.T) {
	byID := map[string]SeedRole{}
	for _, r := range DefaultSystemRoles() {
		byID[r.ID] = r
	}
	if got := byID["role-admin"]; len(got.Capabilities) != 6 || !got.IsSystem {
		t.Fatalf("admin seed wrong: %+v", got)
	}
	if got := byID["role-user"]; len(got.Capabilities) != 3 {
		t.Errorf("user seed should have 3 caps, got %d", len(got.Capabilities))
	}
	req := byID["role-requester"]
	hasReq, hasPlaylists := false, false
	for _, c := range req.Capabilities {
		if c == CapRequest {
			hasReq = true
		}
		if c == CapCreatePlaylists {
			hasPlaylists = true
		}
	}
	if !hasReq || !hasPlaylists || len(req.Capabilities) != 2 {
		t.Errorf("requester seed should be [request, create_playlists], got %+v", req.Capabilities)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/auth/ -run 'TestAllCapabilities|TestValidateCapabilities|TestDefaultSystemRoles' -v`
Expected: FAIL (undefined `CapAutoApprove`, `Description`).

- [ ] **Step 3: Rewrite `capabilities.go`**

```go
package auth

import "errors"

const (
	CapAdmin           = "is_admin"
	CapManageUsers     = "can_manage_users"
	CapManageLibrary   = "can_manage_library"
	CapRequest         = "request"
	CapAutoApprove     = "auto_approve"
	CapCreatePlaylists = "can_create_playlists"
)

var ErrInvalidCapability = errors.New("unknown capability")

type Capability struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AllCapabilities is the fixed registry, in display order. Adding a capability
// here is the only way to introduce one; it is the enforcement contract.
func AllCapabilities() []Capability {
	return []Capability{
		{CapAdmin, "Full administrator", "Complete access; bypasses all restrictions. Opens the Admin area."},
		{CapManageUsers, "Manage users & roles", "Create and edit users, edit roles, and control registration & invites. Opens the Admin area."},
		{CapManageLibrary, "Manage library & integrations", "Configure the music backend, search providers, and downloaders. Opens the Admin area."},
		{CapRequest, "Request music", "Ask to add music to the library. Fulfilled instantly if Auto-approve is also granted; otherwise it waits for an administrator's approval."},
		{CapAutoApprove, "Auto-approve music", "Requests to add music are fulfilled immediately, without approval (one-click add). Implies Request."},
		{CapCreatePlaylists, "Create & edit playlists", "Make and manage their own playlists."},
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
			CapAdmin, CapManageUsers, CapManageLibrary, CapRequest, CapAutoApprove, CapCreatePlaylists,
		}},
		{ID: "role-user", Name: "User", IsSystem: true, Capabilities: []string{
			CapAutoApprove, CapRequest, CapCreatePlaylists,
		}},
		{ID: "role-requester", Name: "Requester", IsSystem: true, Capabilities: []string{
			CapRequest, CapCreatePlaylists,
		}},
	}
}
```

- [ ] **Step 4: Fix the one Go enforcement reference**

In `internal/api/server.go:263`, change `s.requireCapability(auth.CapDownload)` → `s.requireCapability(auth.CapAutoApprove)`. (Same gate — instant add now requires the trust capability.)

- [ ] **Step 5: Run + build + commit**

Run: `go test ./internal/auth/ -v && go build ./...`
Expected: PASS / clean.

```bash
git add internal/auth/capabilities.go internal/auth/capabilities_test.go internal/api/server.go
git commit -m "feat(auth): Seerr-style capability registry (auto_approve/request + descriptions)"
```

---

### Task 2: Idempotent capability remap in EnsureSeed (the upgrade path)

**Files:**
- Modify: `internal/auth/auth.go` (the `EnsureSeed` method — add a remap step at the end)
- Modify: `internal/auth/seed_test.go`

**Interfaces:**
- Consumes: Task 1 (`CapAutoApprove`, `CapRequest`, `CapCreatePlaylists`), existing `s.q.ListRoles`, `s.q.UpdateRole`.
- Produces: `EnsureSeed` now also remaps `can_download`→`auto_approve`, `can_request`→`request` in every role's capability JSON, and ensures `role-requester` contains `can_create_playlists`. Idempotent.

- [ ] **Step 1: Write the failing test**

```go
// internal/auth/seed_test.go — add
func TestEnsureSeedRemapsLegacyCapabilities(t *testing.T) {
	s, q := newTestServiceNoSeed(t)
	ctx := context.Background()
	// simulate an SP1-era install: a role carrying the OLD keys
	if err := q.CreateRole(ctx, db.CreateRoleParams{ID: "role-user", Name: "User", IsSystem: 1, Capabilities: `["can_download","can_request","can_create_playlists"]`}); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateRole(ctx, db.CreateRoleParams{ID: "role-requester", Name: "Requester", IsSystem: 1, Capabilities: `["can_request"]`}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	caps := func(id string) []string {
		r, _ := q.GetRole(ctx, id)
		var c []string
		_ = json.Unmarshal([]byte(r.Capabilities), &c)
		return c
	}
	user := caps("role-user")
	if contains(user, "can_download") || contains(user, "can_request") {
		t.Errorf("old keys not remapped: %v", user)
	}
	if !contains(user, "auto_approve") || !contains(user, "request") {
		t.Errorf("new keys missing: %v", user)
	}
	if !contains(caps("role-requester"), "can_create_playlists") {
		t.Errorf("requester did not gain create_playlists: %v", caps("role-requester"))
	}
	// idempotent: a second run is a no-op, not an error
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatalf("second EnsureSeed failed: %v", err)
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
```

(If a `contains` helper already exists in the test package, reuse it and drop the duplicate. Ensure `encoding/json` is imported in the test file.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/auth/ -run TestEnsureSeedRemapsLegacyCapabilities -v`
Expected: FAIL (old keys still present).

- [ ] **Step 3: Add the remap step at the END of `EnsureSeed`**

Append before `EnsureSeed`'s final `return nil`:

```go
	// Idempotent capability remap for installs seeded before the Seerr-style
	// rename. Rewrites old keys and brings role-requester to its new definition.
	roles, err := s.q.ListRoles(ctx)
	if err != nil {
		return err
	}
	for _, r := range roles {
		var caps []string
		if err := json.Unmarshal([]byte(r.Capabilities), &caps); err != nil {
			continue
		}
		changed := false
		for i, c := range caps {
			switch c {
			case "can_download":
				caps[i], changed = CapAutoApprove, true
			case "can_request":
				caps[i], changed = CapRequest, true
			}
		}
		// role-requester gains create_playlists (its old def lacked it)
		if r.ID == "role-requester" {
			has := false
			for _, c := range caps {
				if c == CapCreatePlaylists {
					has = true
				}
			}
			if !has {
				caps, changed = append(caps, CapCreatePlaylists), true
			}
		}
		if changed {
			b, _ := json.Marshal(caps)
			if err := s.q.UpdateRole(ctx, db.UpdateRoleParams{Name: r.Name, Capabilities: string(b), ID: r.ID}); err != nil {
				return err
			}
		}
	}
```

(`encoding/json` is already imported in `auth.go`.)

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/auth/... && go build ./...`
Expected: PASS / clean.

```bash
git add internal/auth/auth.go internal/auth/seed_test.go
git commit -m "feat(auth): idempotent EnsureSeed remap of legacy capability keys"
```

---

### Task 3: Editable roles + anti-lockout invariant

**Files:**
- Modify: `internal/auth/auth.go` (CreateRole, UpdateRole, DeleteRole, UpdateUserRole, DeleteUser, SetUserDisabled; new errors + helper)
- Modify: `internal/api/roles.go` and `internal/api/users.go` (error→status mapping)
- Modify: `internal/auth/auth_test.go` or add to `internal/api/roles_test.go` / `users_test.go`

**Interfaces:**
- Consumes: Task 1 (`CapAdmin`, `CapAutoApprove`, `CapRequest`), existing `s.q.ListRoles`, `s.q.ListUsers`, `s.GetRegPolicy`.
- Produces: `ErrLastAdmin`, `ErrRoleIsDefault`; private `enabledAdminUserIDs(ctx) (map[string]string, error)`; `UpdateRole`/`DeleteRole` no longer reject `is_system`; create/update normalize `auto_approve ⇒ request`; the four mutations enforce anti-lockout. `ErrSystemRole` is removed (no longer returned).

- [ ] **Step 1: Write failing tests** (API-level, using the existing helpers)

```go
// internal/api/roles_test.go — add
func TestDefaultRolesAreEditable(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	// rename + retag a SYSTEM role (was 409 before) — now allowed
	rr := doPATCH(t, srv, "/api/v1/roles/role-user", tok, `{"name":"Member","capabilities":["request","can_create_playlists"]}`)
	if rr.Code != 200 {
		t.Fatalf("editing a default role should succeed now, got %d (%s)", rr.Code, rr.Body)
	}
}

func TestAutoApproveImpliesRequest(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/roles", tok, `{"name":"DJ","capabilities":["auto_approve"]}`)
	rr := doGET(t, srv, "/api/v1/roles", tok)
	if !bytesContain(rr.Body.Bytes(), `"request"`) {
		t.Fatalf("auto_approve should have implied request: %s", rr.Body)
	}
}

func TestAntiLockout(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	// owner is the only admin; removing is_admin from the Admin role must 409
	rr := doPATCH(t, srv, "/api/v1/roles/role-admin", tok, `{"name":"Admin","capabilities":["can_manage_users","can_manage_library","request","auto_approve","can_create_playlists"]}`)
	if rr.Code != 409 {
		t.Fatalf("stripping is_admin from the only admin role must 409, got %d", rr.Code)
	}
}
```

(Use the existing `bytesContain` helper from the api test package; if absent, mirror the existing string-search helper.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestDefaultRolesAreEditable|TestAutoApproveImpliesRequest|TestAntiLockout' -v`
Expected: FAIL (editing system role currently 409s; no normalization; no lockout guard).

- [ ] **Step 3: Implement the service changes**

Add errors + helper near the other role errors in `auth.go`:

```go
var (
	ErrLastAdmin     = errors.New("would leave no administrator")
	ErrRoleIsDefault = errors.New("role is the registration default")
)

// enabledAdminUserIDs returns userID->roleID for every ENABLED user whose role grants is_admin.
func (s *Service) enabledAdminUserIDs(ctx context.Context) (map[string]string, error) {
	roles, err := s.q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	adminRole := map[string]bool{}
	for _, r := range roles {
		var caps []string
		_ = json.Unmarshal([]byte(r.Capabilities), &caps)
		for _, c := range caps {
			if c == CapAdmin {
				adminRole[r.ID] = true
				break
			}
		}
	}
	users, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, u := range users {
		if u.Disabled == 0 && adminRole[u.RoleID] {
			out[u.ID] = u.RoleID
		}
	}
	return out, nil
}

// normalizeCaps appends request when auto_approve is present (auto_approve implies request).
func normalizeCaps(caps []string) []string {
	hasAuto, hasReq := false, false
	for _, c := range caps {
		if c == CapAutoApprove {
			hasAuto = true
		}
		if c == CapRequest {
			hasReq = true
		}
	}
	if hasAuto && !hasReq {
		caps = append(caps, CapRequest)
	}
	return caps
}
```

`CreateRole` — normalize after validate:

```go
func (s *Service) CreateRole(ctx context.Context, name string, caps []string) (string, error) {
	if err := ValidateCapabilities(caps); err != nil {
		return "", err
	}
	caps = normalizeCaps(caps)
	b, _ := json.Marshal(caps)
	id := "role-" + uuid.NewString()
	return id, s.q.CreateRole(ctx, db.CreateRoleParams{ID: id, Name: name, IsSystem: 0, Capabilities: string(b)})
}
```

`UpdateRole` — drop the `is_system` block; normalize; add lockout check:

```go
func (s *Service) UpdateRole(ctx context.Context, id, name string, caps []string) error {
	if _, err := s.q.GetRole(ctx, id); err != nil {
		return ErrRoleNotFound
	}
	if err := ValidateCapabilities(caps); err != nil {
		return err
	}
	caps = normalizeCaps(caps)
	// anti-lockout: if this edit removes is_admin and would leave no admins, reject
	newHasAdmin := false
	for _, c := range caps {
		if c == CapAdmin {
			newHasAdmin = true
		}
	}
	if !newHasAdmin {
		admins, err := s.enabledAdminUserIDs(ctx)
		if err != nil {
			return err
		}
		remaining := 0
		for _, roleID := range admins {
			if roleID != id {
				remaining++
			}
		}
		if len(admins) > 0 && remaining == 0 {
			return ErrLastAdmin
		}
	}
	b, _ := json.Marshal(caps)
	return s.q.UpdateRole(ctx, db.UpdateRoleParams{Name: name, Capabilities: string(b), ID: id})
}
```

`DeleteRole` — drop `is_system`; keep in-use; add registration-default guard:

```go
func (s *Service) DeleteRole(ctx context.Context, id string) error {
	if _, err := s.q.GetRole(ctx, id); err != nil {
		return nil // not found; no-op
	}
	if n, _ := s.q.CountUsersWithRole(ctx, id); n > 0 {
		return ErrRoleInUse
	}
	if pol, err := s.GetRegPolicy(ctx); err == nil && pol.DefaultRoleID == id {
		return ErrRoleIsDefault
	}
	return s.q.DeleteRole(ctx, id)
}
```

`UpdateUserRole` — add lockout (keep owner guard):

```go
func (s *Service) UpdateUserRole(ctx context.Context, id, roleID string) error {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return ErrRoleNotFound
	}
	if u.IsOwner == 1 && roleID != "role-admin" {
		return ErrOwnerProtected
	}
	r, err := s.q.GetRole(ctx, roleID)
	if err != nil {
		return ErrRoleNotFound
	}
	newHasAdmin := false
	var caps []string
	_ = json.Unmarshal([]byte(r.Capabilities), &caps)
	for _, c := range caps {
		if c == CapAdmin {
			newHasAdmin = true
		}
	}
	if !newHasAdmin {
		admins, err := s.enabledAdminUserIDs(ctx)
		if err != nil {
			return err
		}
		if _, isAdmin := admins[id]; isAdmin && len(admins) == 1 {
			return ErrLastAdmin
		}
	}
	return s.q.UpdateUserRole(ctx, db.UpdateUserRoleParams{RoleID: roleID, ID: id})
}
```

`DeleteUser` and `SetUserDisabled(true)` — add the same last-admin guard after the owner check:

```go
// in DeleteUser, after the ErrOwnerProtected check:
	admins, err := s.enabledAdminUserIDs(ctx)
	if err != nil {
		return err
	}
	if _, isAdmin := admins[id]; isAdmin && len(admins) == 1 {
		return ErrLastAdmin
	}
// then proceed to s.q.DeleteUser(ctx, id)
```

```go
// in SetUserDisabled, when disabled==true, after the ErrOwnerProtected check:
	if disabled {
		admins, err := s.enabledAdminUserIDs(ctx)
		if err != nil {
			return err
		}
		if _, isAdmin := admins[id]; isAdmin && len(admins) == 1 {
			return ErrLastAdmin
		}
	}
```

Remove the now-unused `ErrSystemRole` var and any reference to it.

- [ ] **Step 4: Map the new errors in handlers**

In `internal/api/roles.go`: `handleUpdateRole` and `handleDeleteRole` — map `ErrLastAdmin`→409, `ErrRoleIsDefault`→409, keep `ErrRoleInUse`→409, `ErrInvalidCapability`→400; remove the `ErrSystemRole`→409 mapping. In `internal/api/users.go`: `handleUpdateUser` and `handleDeleteUser` — map `ErrLastAdmin`→409 (alongside the existing `ErrOwnerProtected`→409).

- [ ] **Step 5: Run + commit**

Run: `go test ./... && go build ./... && go vet ./...`
Expected: PASS (update any SP1 test that asserted system-role 409 on edit/delete — e.g. `TestRolesCrudAndProtection` in `roles_test.go` — to the new editable behavior; a default role edit now returns 200, and deletion is blocked by in-use/registration-default rather than by `is_system`).

```bash
git add -A
git commit -m "feat(auth): editable default roles + auto_approve normalization + anti-lockout invariant"
```

---

### Task 4: Frontend capability-key rename + download-gate

**Files:**
- Modify: `web/src/components/download/DownloadAction.tsx` (gate key)
- Modify: `web/src/components/download/DownloadAction.test.tsx`, `web/src/lib/authStore.test.ts`, `web/src/App.test.tsx`, `web/src/components/shell/TopBar.test.tsx`
- Modify: `web/e2e/mocks.ts`, `web/e2e/multiuser.spec.ts`

**Interfaces:**
- Consumes: the renamed backend keys (`auto_approve`, `request`).
- Produces: the FE download affordance gates on `auto_approve`; all FE capability arrays use the new keys.

- [ ] **Step 1: Update tests to the new keys (write-the-failing-test)**

Mechanical key swaps in the test/mocks — `can_download`→`auto_approve`, `can_request`→`request`:
- `DownloadAction.test.tsx`: every `setCaps(['can_download'])`→`setCaps(['auto_approve'])`; the `without can_download` cases keep `setCaps([])`; rename the test titles' "can_download"→"auto_approve".
- `authStore.test.ts`: `capabilities: ['is_admin','can_download']`→`['is_admin','auto_approve']`; the assertion `can('can_request')` (expected false) → `can('request')`; the `['can_request']` example → `['request']`.
- `App.test.tsx:154`: `seedMe(['can_download','can_request'])`→`seedMe(['auto_approve','request'])`.
- `TopBar.test.tsx:125`: `['can_download','can_request','can_create_playlists']`→`['auto_approve','request','can_create_playlists']`.
- `web/e2e/mocks.ts`: the owner capability list `'can_download'`/`'can_request'`→`'auto_approve'`/`'request'`.
- `web/e2e/multiuser.spec.ts`: the capability arrays + the download-gating test (`['can_request']` stays as a non-auto-approve user but rename to `['request']`; the WITH-download case `['can_request','can_download']`→`['request','auto_approve']`); update test names/comments from "can_download"→"auto_approve".

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/download/DownloadAction.test.tsx`
Expected: FAIL (component still reads `can_download`).

- [ ] **Step 3: Update the component**

In `web/src/components/download/DownloadAction.tsx`: change `useAuthStore((s) => s.can('can_download'))` → `s.can('auto_approve')`, and update the two `// ... can_download ...` comments to `auto_approve`.

- [ ] **Step 4: Run the full FE suite + e2e**

Run: `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`
Expected: PASS (vitest all green; e2e all green with the renamed keys).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/download web/src/lib/authStore.test.ts web/src/App.test.tsx web/src/components/shell/TopBar.test.tsx web/e2e
git commit -m "feat(web): gate add-affordance on auto_approve; rename capability keys"
```

---

### Task 5: Frontend — editable role cards + capability descriptions

**Files:**
- Modify: `web/src/lib/usersApi.ts` (Capability type + roleName/capabilities already present)
- Modify: `web/src/components/admin/RolesSection.tsx`
- Modify: `web/src/components/admin/RolesSection.test.tsx`

**Interfaces:**
- Consumes: `GET /capabilities` now returns `{key,label,description}`; backend now allows editing/deleting default roles (Task 3); 409 bodies `{error}` for last-admin / registration-default / in-use.
- Produces: every role card is editable (no `is_system` read-only branch); the capability checklist shows each capability's description; 409 messages surface inline.

- [ ] **Step 1: Update the type + write the failing test**

In `usersApi.ts`, extend the `Capability` interface with `description: string`.

In `RolesSection.test.tsx`: the SP1 test asserted system roles render read-only — flip it. Add/replace with a test that a **default (isSystem) role shows editable controls** (an Edit affordance) and that toggling a capability on a default role calls `updateRole`; and that a capability's description text renders in the checklist. Mock `useCapabilities()` to return entries WITH `description`. (Remove/replace the old "system role shows no editable controls" assertion.)

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/admin/RolesSection.test.tsx`
Expected: FAIL (system roles still render read-only).

- [ ] **Step 3: Implement**

In `RolesSection.tsx`:
- Remove the `!role.isSystem` gate around the Edit/Delete controls so **every** role is editable; keep a small "Default" badge when `role.isSystem` (replace the old read-only treatment).
- In the capability checklist (`RoleForm`/`CapabilityChecklist`), render each capability's `description` as helper text under (or as a `title`/tooltip on) its label, sourced from `useCapabilities()`.
- Map the 409 responses to friendly inline messages (`role="alert"`): last-admin → "This would leave Reverb with no administrator."; registration-default → "This role is the registration default — pick another default first."; in-use → "Reassign this role's users first." (Distinguish by the `error` string from the response, or show a single clear message if the body isn't specific.)
- Token styling only; match the existing card density.

- [ ] **Step 4: Run the full FE suite + e2e**

Run: `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/usersApi.ts web/src/components/admin/RolesSection.tsx web/src/components/admin/RolesSection.test.tsx
git commit -m "feat(web): editable default roles + capability descriptions in the role editor"
```

---

## Self-Review

**Spec coverage (spec §→task):**
- §3 capability registry rename + descriptions → Task 1 ✓
- §4 default roles redefined → Task 1 (seed) + Task 2 (existing-install remap) ✓
- §5 editable roles + deletion guards + anti-lockout → Task 3 ✓
- §6 migration (idempotent remap; Requester gains create_playlists) → Task 2 ✓ (as an EnsureSeed step, not a goose file — noted deviation: data-only, more testable, idempotent)
- §7 enforcement (download gate → auto_approve; normalization; lockout; /capabilities returns description) → Task 1 (gate + description on struct) + Task 3 (normalization, lockout) ✓
- §8 FE (gate key, labels, editable RolesSection, descriptions) → Task 4 (gate/keys) + Task 5 (editable cards + descriptions) ✓
- §9 testing (migration remap, registry, normalization, anti-lockout, editable defaults, enforcement, FE) → distributed across Tasks 1–5 ✓

**Placeholder scan:** none — every step has concrete code or exact key-swaps; the FE component edits name the exact symbols/keys.

**Type consistency:** `CapAutoApprove`/`CapRequest` used identically in Tasks 1–3; `enabledAdminUserIDs` returns `map[string]string` (userID→roleID) and is consumed consistently in UpdateRole/UpdateUserRole/DeleteUser/SetUserDisabled; `normalizeCaps` used by CreateRole+UpdateRole; FE keys `auto_approve`/`request` consistent across Tasks 4–5 and the e2e mocks.

**Note — SP1 tests this changes:** `roles_test.go` `TestRolesCrudAndProtection` (asserted system-role 409 on edit/delete) and any FE `RolesSection.test` "system roles read-only" assertion are intentionally inverted by Tasks 3 and 5 — updating them is in-scope, not a regression.
