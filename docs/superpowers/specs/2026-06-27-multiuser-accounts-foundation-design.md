# Multi-User: Accounts & Identity Foundation — Design Spec

> **Social / Multi-User, Sub-project 1 of 4.** Turn Reverb from a single-admin app into a
> real multi-user one. Today there is **no `users` table at all**: auth is one shared
> admin password and a session just means "someone holds a valid token." This sub-project
> introduces **user accounts, roles, and capabilities**, ties sessions to a user, and
> establishes the invariant that **every request resolves to exactly one authenticated
> user**. It is the prerequisite for everything else in the epic — the request system
> needs a requester, listening history needs a listener, and ownership needs an owner.
> Modeled on the **Jellyfin / Overseerr** posture: don't hard-code a trust level, make it
> **admin-configurable** so one codebase serves both the trusted-LAN-share crowd and the
> semi-public-community crowd.

- **Status:** Approved design (brainstormed 2026-06-27), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Part of epic:** *Social / Multi-User* (listed under "Future — Phase 3+" in `reverb-plan.md`). The epic decomposes into four sub-projects, each its own brainstorm → spec → plan → build cycle:
  1. **Accounts & Identity Foundation** ← *this spec*
  2. **Permissions & Request System** — request → admin approval → download → notify (depends on 1)
  3. **Listening History & Stats** — per-user play tracking, scrobbling, the data base for Wrapped/Discover (depends on 1)
  4. **Richer Social** *(deferred)* — collaborative/shared playlists, public profiles, listening parties, SSO/OIDC (depends on 1/2/3)
- **Builds on:** the existing `internal/auth` service (bcrypt, sessions, setup/login), the `sessions`/`settings` tables + goose migrations (latest `0012`) + sqlc (`make gen`), the chi router + `requireAuth` middleware (`internal/api/middleware.go`), the DB-canonical config + adapter registries, the managed-playlists system (`synced_playlists`), and the download manager. React 19 / TS SPA with design-token styling.

---

## 1. Goals & Non-Goals

### Goals
1. **Real accounts.** A first-class `users` table (Reverb-local, bcrypt). Multiple named users, each with a role.
2. **One invariant: always a current user.** Every authenticated request resolves to exactly one `User` in request context. No anonymous path.
3. **Admin-configurable trust, not a hard-coded level.** Roles + capabilities the admin tunes, plus configurable signup — so the same build serves a private LAN share and a semi-public community.
4. **Setup-and-go defaults.** Ship sensible default roles (Admin / User / Requester) seeded on first run; a fresh install is usable without touching permission config.
5. **Clean, non-breaking upgrade.** An existing single-admin install keeps working — the admin just adds a username to their existing password. An old auth-disabled install is prompted once to create the owner account.
6. **Self-service + admin control.** Every user gets an **Account page**; admins get a full **Users management page** (users, roles, invites, registration policy).
7. **Defense-in-depth authorization.** Capabilities enforced at the backend on every gated route; the UI hides what you can't do, but the server is the source of truth.

### Non-Goals (this sub-project)
- **The request system itself.** `can_request` is modeled and `can_download` is enforced as a 403 gate, but the "else, file a request" workflow + approval queue is **Sub-project 2**.
- **Listening history / scrobbling.** Sub-project 3.
- **Playlist sharing / collaboration / public profiles.** Playlists become **owner-private only** here; all visibility/sharing is deferred to Richer Social.
- **OIDC / SSO / external identity.** Reverb-local accounts only. (SSO/OIDC is already parked in "Future Stuff".)
- **Mapping Reverb users onto Navidrome/Subsonic users.** Reverb stays the *single* Subsonic client using its configured backend credentials. Reverb identities are Reverb-local.
- **Per-user app preferences** (theme, playback) — the model leaves room (an Account page slot) but no `user_settings` table is built until a real per-user pref exists (YAGNI).
- **Per-user one-off capability overrides.** A custom role covers the same need; not building two parallel permission systems.
- **Per-role download quotas / auto-approve.** Belongs with the request system (SP2).

---

## 2. Locked Product Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Identity source | **Reverb-local accounts** (own `users` table, bcrypt) | Reverb is the single Subsonic client; users are Reverb-local. OIDC/SSO deferred. |
| Trust model | **Admin-configurable** (Jellyfin/Seerr posture) | "Use cases may vary" — one build, admin picks the posture, not us. |
| Permission model | **Code-defined capabilities + DB roles that bundle them; one role per user** | Capabilities are the enforcement contract (code); roles are admin-composable. Setup-and-go via seeded defaults; flexibility via custom roles. |
| Custom roles | **Admin can create/edit roles**; default roles seeded | Covers varied deployments without a full RBAC editor. System roles are protected. |
| Per-user overrides | **Deferred** | A custom role covers the need; avoid two parallel systems. |
| Capability storage | **JSON array on the role row, encapsulated behind `auth.Service`** | Capabilities are a fixed code enum, read as a whole set every request, at tiny scale — store-together-what-you-read-together. No real parent table to FK. Reversible to a join table later via a contained data migration because storage is hidden behind `HasCapability`. |
| Onboarding | **Configurable, secure default**: admin-create always; opt-in open signup + invite links; configurable default role | Safe out of the box (admin-create-only, like Jellyfin); flexible when the admin opts in. |
| No-login mode | **Dropped — login always required** | Strengthens the "always a current user" invariant; removes the anonymous edge case. Existing auth-disabled installs are prompted once to create the owner. |
| Single-admin migration | **Existing `admin_password_hash` → user `admin`, Admin role, `is_owner=true`, same hash**; existing sessions back-filled | Smooth upgrade — current password keeps working, no forced logout. |
| Playlist ownership | **Owner-private only** in foundation | Establishes `owner_user_id` cleanly; sharing/collab deferred to Richer Social. |
| Downloads | **Shared queue + `initiated_by` attribution** | Queue is a server resource (one disk/library); attribution captured now so SP2 slots in. |
| Owner safety | **Owner account cannot be deleted or demoted out of Admin** | Prevents an admin from locking themselves out of their own server. |

---

## 3. Architecture

### 3.1 The three concepts

```
Capabilities (code)         Roles (DB, JSON-bundled caps)        Users (DB)
─────────────────────       ────────────────────────────        ──────────────────
is_admin                    Admin     → [all caps]  (system)     id, username (UNIQUE),
can_manage_users            User      → can_download,            password_hash, role_id,
can_manage_library                      can_request,             is_owner, disabled,
can_download                            can_create_playlists     created_at, updated_at
can_request                 Requester → can_request  (system)
can_create_playlists        <custom>  → admin-defined bundle     (exactly one role per user)
```

- **Capabilities** are a fixed registry defined in Go (`internal/auth/capabilities.go`). They are the only thing the backend enforces. New capabilities are added in code as later sub-projects need them; the registry is the contract.
- **Roles** are DB rows: `roles(id, name UNIQUE, is_system, capabilities)` with `capabilities` a JSON array of capability keys. Three system roles are seeded (Admin/User/Requester). Admin can create custom roles and edit a non-system role's capability set. System roles are undeletable and their capabilities are read-only (Admin always retains `is_admin`).
- **Users** have exactly one `role_id`. Capability resolution: load user → load role → unmarshal capability set → `HasCapability(user, key)`. All of this is encapsulated in `auth.Service`; callers never see JSON.

### 3.2 The current-user invariant

`requireAuth` resolves the session cookie/token → loads the user + role → builds a `CurrentUser` value (id, username, role, resolved capability set) → injects it into request context. Two thin wrappers compose on top:
- `requireCapability(key)` → 403 if the current user lacks `key`.
- `requireAdmin` = `requireCapability(is_admin)`.

The `auth_disabled` bypass is **deleted**. There is no code path that serves an authenticated route without a `CurrentUser`.

### 3.3 Data model changes (one new additive goose migration, `0013_users_roles_invites.sql`)

New tables:
```sql
roles (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  is_system    INTEGER NOT NULL DEFAULT 0,
  capabilities TEXT NOT NULL DEFAULT '[]',   -- JSON array of capability keys
  created_at   INTEGER NOT NULL DEFAULT (unixepoch()),
  updated_at   INTEGER NOT NULL DEFAULT (unixepoch())
)

users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role_id       TEXT NOT NULL REFERENCES roles(id),
  is_owner      INTEGER NOT NULL DEFAULT 0,
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
  updated_at    INTEGER NOT NULL DEFAULT (unixepoch()),
  last_seen     INTEGER
)

invites (
  id         TEXT PRIMARY KEY,
  code       TEXT NOT NULL UNIQUE,
  role_id    TEXT REFERENCES roles(id),   -- NULL → use default_role_id at redemption
  created_by TEXT REFERENCES users(id),
  expires_at INTEGER,                       -- NULL → no expiry
  used_by    TEXT REFERENCES users(id),
  used_at    INTEGER,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
)
```

Altered tables (additive columns):
- `sessions` += `user_id TEXT REFERENCES users(id)`.
- `download_jobs` += `initiated_by TEXT REFERENCES users(id)` (nullable — pre-existing jobs have none).
- `synced_playlists` += `owner_user_id TEXT REFERENCES users(id)`.

Registration policy lives in the existing global `settings` table (string keys): `signup_enabled` (default `false`), `invites_enabled` (default `false`), `default_role_id` (default = the seeded **User** role id).

> **Generated code:** all `internal/store/db/*.sql.go` + `models.go` are owned by sqlc — edit the `.sql` queries and run `make gen`, never hand-edit. The migration is a new goose file; never edit applied migrations (`0001`–`0012`).

### 3.4 Data migration (runs inside `0013` / startup seed)

1. Seed the three system roles (Admin = all caps; User = `can_download,can_request,can_create_playlists`; Requester = `can_request`). Seed `default_role_id` → User.
2. **If `admin_password_hash` exists in `settings`:** create user `admin` with that exact hash, Admin role, `is_owner=true`. Back-fill all existing non-expired `sessions.user_id` to this user. (The legacy `admin_password_hash` key may be left in place, unused, or cleaned up — implementer's call; it is no longer read for auth.)
3. **If it does not exist** (an install that ran auth-disabled with no password): create no user. `IsSetupRequired` (now "do any users exist?") returns true → next load lands on first-run setup.
4. The `auth_disabled` setting is no longer read; the bypass is removed from `requireAuth`.

`IsSetupRequired` changes from "is `admin_password_hash` absent" to **"does the `users` table have zero rows."**

---

## 4. Authorization & ownership enforcement (scope for *this* sub-project)

| Capability | Enforced now? | Where |
|---|---|---|
| `is_admin` / `can_manage_users` | **Yes** | `/users`, `/roles`, `/invites`, registration-policy routes; the Users page |
| `can_manage_library` | **Yes** | adapter / library backend / downloader / search config routes (today merely "authed" → now capability-gated) |
| `can_create_playlists` | **Yes** | playlist create + mutation routes |
| `can_download` | **Yes, as a 403 gate** | `POST /downloads`, `POST /downloads/batch`. The "else file a request" branch is **SP2**; foundation just enforces the gate so SP2 slots in. |
| `can_request` | Model only | the request workflow is **SP2** |

**Ownership rules:**
- **Playlists** — every read/write checks `owner_user_id == currentUser.id` (admins bypass). List endpoints return only the current user's playlists. Migration assigns existing playlists to the owner admin. Private-only — no sharing.
- **Downloads** — queue stays shared; each new job records `initiated_by = currentUser.id`. No per-user queue filtering yet (SP2); attribution captured now.
- **Server config** (adapters/backends/registration policy) — global, admin-only.

**Threading:** handlers that create/mutate owned data read `CurrentUser` from context. **No handler trusts a user id from the request body.** This single chokepoint keeps ownership honest.

**Self vs. admin boundary:** a non-admin may change **their own** password (and future prefs) but cannot touch roles, other users, or registration policy. An admin manages everyone — except the **owner** account cannot be deleted or demoted out of Admin.

---

## 5. API surface

All routes under the existing `/api/v1` prefix. **Changed** = pre-existing route whose contract changes; **New** = added.

### Auth & self-service
| Method | Path | Notes |
|---|---|---|
| POST | `/auth/setup` | **Changed** — first-run, `{username, password}` → creates owner (Admin, `is_owner`). 409 if a user already exists. |
| POST | `/auth/login` | **Changed** — `{username, password}` (was password-only). Sets `reverb_session` cookie carrying a session with `user_id`. |
| POST | `/auth/logout` | Unchanged mechanics. |
| GET | `/auth/me` | **New** — current user + resolved capabilities (+ setup/registration-policy hints for the FE). |
| POST | `/auth/signup` | **New** — `{username, password, invite?}`. 403 unless `signup_enabled` (or a valid `invite` when `invites_enabled`). Assigns invite's role, else `default_role_id`. |
| GET | `/account` | **New** — own profile (username, role, capabilities, member-since). |
| PATCH | `/account` | **New** — editable own fields. |
| POST | `/account/password` | **New** — `{current, new}`; verifies current before setting. |
| POST | `/account/logout-all` | **New** — clears the current user's other sessions. |

### Admin — users / roles / invites / policy (all `can_manage_users` / `is_admin`)
| Method | Path | Notes |
|---|---|---|
| GET | `/users` | list (username, role, status, created, last-seen) |
| POST | `/users` | create `{username, password, role_id}` |
| PATCH | `/users/{id}` | edit (role, disabled) — owner guardrails enforced |
| DELETE | `/users/{id}` | delete — 409 on owner |
| POST | `/users/{id}/password` | admin reset |
| GET | `/roles` | list roles + capabilities |
| POST | `/roles` | create custom role `{name, capabilities[]}` (validated against the registry) |
| PATCH | `/roles/{id}` | edit name/capabilities — system roles: read-only capabilities |
| DELETE | `/roles/{id}` | delete — 409 on system role or role-in-use (or reassign policy: 409 + message) |
| GET/PATCH | `/settings/registration` | `{signup_enabled, invites_enabled, default_role_id}` |
| GET | `/invites` | list active/used |
| POST | `/invites` | generate `{role_id?, expires_at?}` → returns code/link |
| DELETE | `/invites/{id}` | revoke |

The capability registry is also exposed (e.g. via `/roles` metadata or a small `/capabilities` endpoint) so the role editor can render a checklist with human labels.

---

## 6. Frontend surfaces

Design-token styling only (no raw hex, no `text-black`/`text-white`); match the density and idioms of the existing settings + detail-page components. All gates are **defense-in-depth** — UI hides, backend enforces.

### ① Account page (`/account`, every user)
- **Profile:** username, role badge, member-since, and your **capability chips** (read-only — so a user understands what they can do).
- **Security:** change password (current + new + confirm, inline validation).
- **Sessions:** sign out; "sign out everywhere" → `/account/logout-all`.
- Reserves a slot for per-user prefs (theme/playback) for when those land — not populated now.

### ② Admin → Users page (`/settings/users`, `can_manage_users`)
- **Users table:** username, role, status (active/disabled), created, last-seen; row actions → edit role, reset password, disable/enable, delete. The **owner** row shows a lock + tooltip and exposes no destructive action.
- **Create user** (modal): username, password, role select.
- **Roles panel:** list roles with capability chips; create/edit a custom role via a **capability checklist** (rendered from the registry metadata); system roles render read-only.
- **Registration policy** (card): toggles for open signup + invites; default-role picker.
- **Invites** (shown when enabled): generate (role + expiry) → copyable link; list active/used with revoke.

### ③ Auth flow pages
- **Login** — username + password (add the username field).
- **First-run setup** — username + password (creates the owner).
- **Signup / invite-redeem** — shown per registration policy; invite link prefills the code.

### ④ App-wide gating
- An **auth store** hydrated from `GET /auth/me` (current user + capabilities).
- Capability-driven UI: non-admins don't see Library/Server config or the Users page; `can_download` toggles the download affordance (the "Request" branch fills in at SP2).

---

## 7. Backend shape (Go)

- `internal/auth/capabilities.go` — the capability registry (keys + human labels + the default-role bundles) and `HasCapability`.
- `internal/auth/auth.go` — extend `Service`: user CRUD, role CRUD, invite CRUD, `Login(username, pw)`, `CreateSession(userID)`, `ValidateToken → (CurrentUser, ok)`, registration-policy accessors. Remove `IsAuthDisabled`/`SetAuthDisabled`. The `Querier` interface grows the new queries; keep the service the single seam over storage representation (JSON capabilities never leak out).
- `internal/store/queries/{users,roles,invites}.sql` + extended `sessions.sql` → `make gen`.
- `internal/api/middleware.go` — `requireAuth` resolves `CurrentUser` into context; add `requireCapability` / `requireAdmin`; context accessor `currentUser(r)`. Delete the auth-disabled bypass.
- `internal/api/` — new handler files for account, users, roles, invites, registration policy; update `auth` handlers (setup/login/me). Wire routes in `server.go`. Apply `can_manage_library` gating to existing adapter/config routes and `can_download` to download routes and `can_create_playlists`/ownership to playlist routes.

---

## 8. Testing (the green gate + this sub-project's coverage)

Gate (must be green before merge): from root `go test ./...` && `go build ./...` && `go vet ./...`; from `web/` `npx vitest run` && `npx tsc --noEmit` && `npm run build` && `npm run e2e` (e2e stays green).

Added coverage:
- **Migration:** single-admin install → owner user with same hash; sessions back-filled (no forced logout); auth-disabled-no-password install → setup required.
- **Auth flows:** setup (409 when a user exists), login (username+password; bad creds; disabled user rejected), logout, `/auth/me` shape, signup gated by policy, invite redemption (valid/expired/used → correct role).
- **Authorization:** capability 403s on each gated route; `can_download` gate returns 403; admin bypass on playlist ownership.
- **Ownership:** playlist list/read/write scoped to owner; handler ignores body-supplied user id.
- **Roles:** create/edit/delete custom role; **system-role protection** (undeletable, capabilities read-only, Admin keeps `is_admin`); delete-role-in-use rejected.
- **Owner guardrail:** owner cannot be deleted or demoted out of Admin.
- **Account:** change-password verifies current; logout-all clears sibling sessions.
- **FE:** auth store gating (admin-only surfaces hidden for non-admins), Account page password validation, Users page role/capability editor, login/setup username flows; e2e covers setup → login → admin-creates-user → that user logs in.

---

## 9. Rollout & migration summary

- One additive goose migration (`0013`) + a startup seed/data-migration step. No applied migration is edited.
- Existing **single-admin** install: zero data loss; the admin now logs in with `admin` + their existing password; no forced logout.
- Existing **auth-disabled** install: prompted once to create the owner account.
- Fresh install: first-run setup creates the owner; default roles seeded; signup/invites off by default.
- Branch → green gate → fast-forward merge to **local `main`**; the user pushes + rebuilds the Docker image to verify on `soulkiller:8090`. (No `git push` without explicit go-ahead.)

---

## 10. Open items deferred to later sub-projects (recorded so they aren't lost)

- **Request workflow + approval queue + quotas/auto-approve** → SP2 (the `can_request` payoff; `can_download` 403 gate is the hook).
- **Per-user download queue filtering / "my downloads"** → SP2 (attribution captured here).
- **Listening history / scrobbling** → SP3.
- **Playlist sharing / collaboration / public profiles / listening parties** → SP4 (Richer Social).
- **OIDC / SSO** → Future Stuff.
- **Per-user app preferences** (`user_settings` table) → built when the first real per-user pref ships.

---

*Reverb — own your music, again. Now with friends.*
