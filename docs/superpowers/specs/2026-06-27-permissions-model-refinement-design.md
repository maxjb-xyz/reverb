# Permissions Model Refinement — Seerr-style Acquisition + Editable Roles — Design Spec

> **A focused correction to the SP1 (Accounts & Identity Foundation) permission model**, prompted by reviewing the shipped Admin → Users UI on the live install. Three problems surfaced: (1) **`can_download` and `can_request` read as two independent, redundant toggles** — "if you can download you'd never need to request"; (2) the default roles are **frozen** (system roles render read-only, no edit/delete); (3) the **Requester role is crippled** — it has *only* "Request tracks" and loses unrelated abilities like creating playlists, even though needing approval to add music says nothing about whether you can make a playlist. This spec re-frames acquisition the way **Overseerr/Jellyseerr** does (one action — *Request* — with an **Auto-approve** trust modifier; "download" disappears as a concept), separates the *acquisition* axis from *everything else*, and makes roles editable behind a single anti-lockout invariant.

- **Status:** Approved design (brainstormed 2026-06-27), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Refines:** `docs/superpowers/specs/2026-06-27-multiuser-accounts-foundation-design.md` (SP1). SP1's identity/auth/migration machinery is unchanged; only the **capability registry, the default roles, and the role-editing rules** change.
- **Builds on:** the SP1 capability registry (`internal/auth/capabilities.go`), `auth.Service` role CRUD + `RolesSection`/`UsersSection` admin UI, the `roles` table (JSON-array capabilities), `EnsureSeed`, and the route/UI capability gates.
- **Part of epic:** *Social / Multi-User*. This is a **pre-SP2 correction**, not a new sub-project. The request **submission + approval inbox** workflow remains **SP2**; this spec only makes the *model* coherent so SP2 slots in.

---

## 1. Goals & Non-Goals

### Goals
1. **One acquisition action, with a trust modifier.** Adopt Seerr's framing: the user action is **Request**; **Auto-approve** means your request is fulfilled instantly (= today's one-click "download"). The word "download" disappears from the permission model. This kills the "two redundant toggles" confusion.
2. **Separate axes.** *How you acquire music* (request / auto-approve) is independent of *everything else* (create playlists, play/browse). A user who must get approval to add music is otherwise a full member.
3. **Fix the default roles.** Requester regains Create-playlists; User is the trusted "adds instantly" member; Admin is everything.
4. **Editable roles.** Default roles become editable and renamable like any other; the frozen `is_system` read-only treatment is dropped. Deletion/edits are bounded by **references** (in-use, registration-default) and a single **anti-lockout invariant** (always ≥1 administrator).
5. **Documented capabilities.** Every capability carries a human description; the admin UI explains what each does — including that *any* of the three admin/manage capabilities opens the Admin area.
6. **Clean migration.** Existing installs (the maintainer's `soulkiller`) upgrade with zero data loss: capability keys remap, Requester gains Create-playlists, the admin user / password / playlists / registration policy are untouched.

### Non-Goals (this pass)
- **The request submission + approval inbox workflow** — a non-auto-approve user's "add" creating a pending request that an admin approves. That is **SP2**, unchanged. In v1 after this pass, `auto_approve` is the working acquisition path; `request`-without-auto-approve is the wired-for hook whose UI action lands in SP2.
- **A granular "approve others' requests" capability** (`manage_requests`). Admins approve via `is_admin`; a granular version can come *with* the SP2 workflow if actually wanted. Not adding a forward-looking chip now.
- **Per-user capability overrides, quotas, auto-expiry of requests** — SP2+ / deferred.
- **Any change to SP1's auth/session/identity/migration core**, the owner guardrails, ownership scoping, or the three unrelated deferred cleanups (fail-open `PlaylistOwner`, dead `ExternalRow`, stale Settings "Account" tab) — those are tracked separately.
- **Backward-compatibility shims for the old capability keys.** The feature only just shipped to a single personal install; the rename is a clean data migration, no dual-key support.

---

## 2. Locked Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Acquisition framing | **Seerr-faithful: one action (`request`) + `auto_approve` modifier; drop "download"** | "Downloading is just an auto-approved request." Removes the redundant-toggle confusion; matches the model the maintainer referenced. |
| Axis separation | **Acquisition is independent of playlists/play** | Needing approval to add music ≠ losing unrelated abilities. Fixes the crippled Requester. |
| `request` scope | **A capability, default-on for member roles** (removable for a listen-only role) | Mirrors Seerr's removable "Request" permission; lets an admin make a play-only account. |
| `auto_approve` | **Replaces `can_download`; implies `request`** | Same enforcement point as today's download gate; semantically "your request is pre-approved." |
| `manage_requests` | **Not added this pass** | Avoid a forward-looking chip; admins approve via `is_admin`; revisit in SP2. |
| Default roles | **Admin = all; User = auto_approve+request+create_playlists; Requester = request+create_playlists** | User = today's trusted downloader; Requester = full member who must get approval to add music. |
| Role editability | **All roles editable + renamable; `is_system` no longer locks** | Directly fixes "why can't I edit the defaults." |
| Deletion guard | **Block if in-use OR is the registration-default role** | Referential integrity; replaces the blanket system-role ban. |
| Anti-lockout | **Reject any mutation that would leave 0 users with `is_admin`** (role edit, user-role change, user delete/disable) | Single robust invariant; owner stays delete/demote-protected from SP1. |
| Capability docs | **Every capability has a label + description, surfaced in the UI** | "Download vs Request" with no copy was unreadable. |
| Migration | **One additive goose migration: JSON-remap keys across all roles + add create_playlists to Requester** | Zero-loss upgrade; covers custom roles generically. |

---

## 3. The Capability Registry (revised)

`internal/auth/capabilities.go` — keys, labels, and **descriptions** (new). The registry is still the fixed, code-defined enforcement contract.

| Key | Label | Description |
|---|---|---|
| `is_admin` | Full administrator | Complete access; bypasses all restrictions. Opens the Admin area. |
| `manage_users` | Manage users & roles | Create and edit users, edit roles, and control registration & invites. Opens the Admin area. |
| `manage_library` | Manage library & integrations | Configure the music backend, search providers, and downloaders. Opens the Admin area. |
| `request` | Request music | Ask to add music to the library. Fulfilled instantly if **Auto-approve** is also granted; otherwise it waits for an administrator's approval. |
| `auto_approve` | Auto-approve music | Requests to add music are fulfilled **immediately, without approval** (one-click add). Implies **Request**. |
| `create_playlists` | Create & edit playlists | Make and manage their own playlists. |

**Renames from SP1:** `can_download` → `auto_approve`; `can_request` → `request`. Removed: none. Added: none (descriptions are new metadata on existing+renamed keys).

**Baseline (not capabilities):** playing and browsing the library are available to every authenticated user and are not gated.

**Admin-area entry:** the existing manager predicate stays `is_admin || manage_users || manage_library`; the descriptions now make that visible. (Each Admin sub-area remains gated by its specific capability.)

**Capability metadata shape:** `Capability { Key, Label, Description string }` — `Description` is added; `GET /capabilities` returns it so the role editor can render help text.

---

## 4. The Default Roles (revised)

Seeded by `DefaultSystemRoles()` (fixed IDs unchanged: `role-admin`, `role-user`, `role-requester`):

| Role | Capabilities | Intent |
|---|---|---|
| **Admin** | `is_admin, manage_users, manage_library, request, auto_approve, create_playlists` | Owner / co-admins — everything. |
| **User** | `auto_approve, request, create_playlists` | **Trusted** member: adds music instantly (today's "can download" member), makes playlists. |
| **Requester** | `request, create_playlists` | Member who must get an admin's approval to add music — **but still plays, browses, and makes playlists.** No longer a dead account. |

`is_system` remains as metadata (drives a subtle "Default" badge and a possible future "reset to defaults") but **does not** make a role read-only or undeletable.

---

## 5. Editable Roles & the Anti-Lockout Invariant

**Editing:** any role's `name` and `capabilities` can be changed (defaults included). `auto_approve` selected without `request` is normalized to also include `request` (auto-approve implies the action it approves) — enforced server-side on save.

**Deletion:** a role may be deleted only if **no users are assigned to it** AND **it is not the current registration-default role** (`default_role_id`). Otherwise → HTTP 409 with a clear message (e.g. "Reassign its users first" / "Pick a different default role first"). The old "system roles can never be deleted" rule is removed.

**Anti-lockout invariant (the one real guard):** the system must always have **at least one enabled user with `is_admin`**. Evaluated server-side on every mutation that can reduce administrators:
- editing a role's capabilities (would this remove `is_admin` from a role whose members are the last admins?),
- changing a user's role,
- deleting or disabling a user.

A mutation that would result in **zero** enabled `is_admin` users is rejected → HTTP 409 ("This would leave Reverb with no administrator"). The SP1 **owner** protections (the `is_owner` user cannot be deleted or demoted out of Admin) remain and are the first line of defense for the single-admin case; the invariant generalizes it to multi-admin setups and to *role-capability* edits (which SP1 didn't guard because system roles were frozen).

---

## 6. Migration (one additive goose migration, e.g. `0014_capability_rename.sql`)

Runs once on upgrade; idempotent-safe; **never edits applied migrations** (`0001`–`0013`):
1. **Remap capability keys in every role's stored JSON** (`roles.capabilities` is a JSON-array text column): replace the substring `"can_download"` → `"auto_approve"` and `"can_request"` → `"request"`. A SQLite `UPDATE ... SET capabilities = replace(replace(capabilities,'can_download','auto_approve'),'can_request','request')` covers all roles generically (custom roles too).
2. **Add `create_playlists` to the Requester role** (`role-requester`) if absent — bring the seeded role to its new definition. (The Admin/User default roles already contain `create_playlists`, so only Requester needs the additive cap; its `request` key is handled by step 1's remap of the old `can_request`.)
3. Everything else — the `users`/`sessions`/`invites` tables, the admin account + password, playlists, `synced_playlists.owner_user_id`, registration policy settings — is **untouched**.

`DefaultSystemRoles()` is updated to the §4 definitions so **fresh** installs seed correctly; `EnsureSeed`'s "skip role if it already exists" means existing installs rely on the migration above (not on a re-seed) to update their rows.

> **Generated code / migrations:** `internal/store/db/*.sql.go` stays sqlc-owned; the JSON-remap is a data-only migration needing no query/schema change. New goose file only; no edits to `0001`–`0013`.

---

## 7. Enforcement & Backend changes

- `internal/auth/capabilities.go`: rename the two key consts (`CapDownload`→`CapAutoApprove` = `"auto_approve"`, `CapRequest`→`CapRequest` value `"request"`), add `Description` to `Capability` + fill all six, update `DefaultSystemRoles()` to §4.
- **Enforcement sites:** the download-create gate (`POST /downloads`, `/downloads/batch`) now checks `auto_approve` instead of `can_download` (same behavior — instant add requires the trust capability). The playlist gate stays `create_playlists`; the manager/admin gates stay `is_admin || manage_users || manage_library`.
- `auth.Service`: `CreateRole`/`UpdateRole` normalize `auto_approve ⇒ request`; add the **anti-lockout** check to `UpdateRole` (capability edits), `UpdateUserRole`, `DeleteUser`, `SetUserDisabled`; relax `DeleteRole`/`UpdateRole` so `is_system` no longer blocks (replace with the in-use / registration-default / lockout guards). New error e.g. `ErrLastAdmin`.
- `GET /capabilities` returns `Description`.

## 8. Frontend changes

- `RolesSection.tsx`: render **all** roles with an editable capability **checklist showing each capability's description** (from `GET /capabilities`); remove the read-only treatment for `is_system` roles (keep a small "Default" badge); wire create/edit/delete for every role; surface the 409 messages (in-use, registration-default, last-admin) inline.
- `UsersSection.tsx` / capability chips: reflect the renamed labels ("Auto-approve music", "Request music"); no "Download" copy anywhere.
- `authStore.ts` + gating: the download affordance gate keys off **`auto_approve`** (was `can_download`); the manager predicate is unchanged. `DownloadAction`/`DownloadPopover`: the instant-add control shows for `auto_approve` holders exactly as the download control does today (behavior unchanged — only the capability key it reads).
- Capability keys referenced in tests/mocks updated to the new keys.
- Design tokens only; match existing admin density.

## 9. Testing

Gate (must stay green): root `go test ./... && go build ./... && go vet ./...`; `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.

Added/changed coverage:
- **Migration:** a role seeded with old keys (`can_download`/`can_request`) ends up with `auto_approve`/`request`; Requester ends up with `create_playlists`; an existing admin user keeps `is_admin` and can still log in (no lockout from the remap).
- **Capability registry:** six capabilities, each with a non-empty description; `auto_approve` and `request` present; old keys absent.
- **Role normalization:** creating/updating a role with `auto_approve` but not `request` persists both.
- **Anti-lockout:** editing the Admin role to drop `is_admin` (when it's the only admin) → 409; changing the sole admin's role to a non-admin role → 409; deleting/disabling the last admin → 409; a multi-admin setup allows removing one.
- **Editable defaults:** a default (system) role can be renamed and have a capability toggled (no longer read-only); a default role can be deleted only when unused and not the registration default.
- **Enforcement:** the download endpoint requires `auto_approve` (a `request`-only user → 403); the Requester role can create playlists (regression test for the crippled-role bug).
- **FE:** RolesSection renders editable checklists with descriptions for default roles; capability labels show no "Download" copy; download affordance gated on `auto_approve`. e2e capability mocks use the new keys.

## 10. Rollout

- Branch `feat/permissions-refinement` (off `main`, which has SP1) → green gate → fast-forward to local `main`; the maintainer pushes + rebuilds to verify on `soulkiller` (the migration + the editable role cards + the renamed capabilities). No `git push` without explicit go-ahead.
- After this lands and is verified, the epic proceeds to **SP2 — the request submission + approval inbox** on top of the now-coherent model.

---

*Reverb — own your music, again. Now the permissions make sense.*
