# Artist-Level "Request All" — Design

**Status:** Approved by "just do everything" (2026-06-28) — design decisions made by Claude, documented here for the record. Feature 1 of 3 closing out the SP2 request-system backlog (then: request quotas, then push-notifications-for-approvals).

**Goal:** Let a user request an artist's whole discography in one action — fanning out into individual album requests through the existing request/approval/fulfillment system.

**Tech stack:** Go (request service + chi API) + React/TS (Artist page). Builds entirely on the shipped album-request infra (`kind:'album'` requests → the album downloader chain).

---

## Decisions (made, not deferred)

1. **Fan-out into N album requests, no new request kind/entity.** "Request all" creates one `kind:'album'` request per discography entry. Reuses the album-request approval + fulfillment paths verbatim. The user sees the artist's albums appear as album requests in `/requests`.
2. **Server-side batch endpoint** (one call, not N), so dedup + (future) quota enforcement live in one place.
3. **FE sends only NOT-fully-owned entries.** The Artist page already has per-album coverage (owned/total); it filters to `owned < total` and sends those. No point requesting albums fully in the library.
4. **Whole discography** (albums + singles — a single is a 1-track album, requested the same way). Independent of the page's album/single view filter.
5. **Dedup server-side** per `(requested_by, source, external_id)` (the existing `GetOpenRequestByItem`); an already-open album request for the same album is skipped, not duplicated.

---

## Architecture

### Backend — batch create

A new endpoint `POST /api/v1/requests/batch` (in the `request`-gated route group, same as `POST /requests`):
- Body: `{ "items": [ <RequestItem>, ... ] }` — each item is an album `RequestItem` (`kind:'album'`, `source`, `externalId`=album id, `title`+`album`=album name, `artist`, `coverUrl`, `trackCount`).
- For EACH item, run the SAME logic as the single `handleCreateRequest`: `Create` (which dedups via `GetOpenRequestByItem`) → if it already existed, count as skipped → else if the caller `auto_approve`s, enqueue to the album chain + `MarkApproved` → else `NotifyPending`. Extract that per-item logic from `handleCreateRequest` into a shared helper `(s *Server) createOneRequest(ctx, cu, item) (req, created bool, err error)` so the single and batch handlers share it (DRY).
- Returns `{ created: int, skipped: int, requests: []Request }` (skipped = already-open dups). Per-item failures are collected and logged but don't abort the batch (best-effort; the response reports created/skipped counts).
- This is the natural enforcement point for the request quota that's coming next — note a TODO seam (don't implement quota here; feature 2).

### Frontend — Artist page "Request all"

- A **"Request all"** button in the Artist page header (next to the existing controls), gated on `can('request')`.
- On click: collect the discography entries that are NOT fully owned (`coverage.owned < coverage.total`, or no coverage resolved yet → treat as not-owned), build an album `RequestItem` for each (`source`, `externalId`=album id, `title`+`album`=name, `artist`=the artist name, `kind:'album'`, `coverUrl`, `trackCount`=total tracks), and show a disclosure: "Request all N albums by <artist> that aren't in your library?" On confirm, `POST /requests/batch`.
- On success, a toast: `auto_approve` → "Requested N albums" (they enqueue immediately); `request`-only → "Requested N albums — pending approval". If 0 not-owned entries, the button is disabled/hidden (nothing to request).
- Reuse the existing toast + disclosure patterns. Tokens only.

### Data flow

Click → FE gathers not-owned discography album items → `POST /requests/batch` → backend loops `createOneRequest` per item (dedup + auto/pending) → response summary → toast. Each created request is a normal album request: `auto_approve` enqueues an album-chain job (spotDL album or Lidarr); `request`-only sits pending in the manager Approval queue; the existing tracker flips each on its job's complete/failed; `/requests` renders each (with the "Album · N tracks" cue).

## Error handling

- Per-item create failure → logged, counted, batch continues (best-effort); the toast reflects the created count.
- No album downloader configured → each `auto_approve` enqueue errors (as today); the batch reports them as failed; the toast notes some couldn't be queued.
- Empty/owned-everything discography → button disabled; no request.

## Out of scope (explicit)

- **Request quotas** — feature 2 (next). The batch endpoint is where it'll be enforced; left as a seam.
- **Bulk-approve in the manager queue** — N pending album requests for a `request`-only user means a manager approves each individually for now. A "approve all from <user>" / "approve all for <artist>" bulk action is a possible later follow-up, not built here.
- **A single artist-request entity / parent-child fulfillment** — rejected; flat album requests.

## Testing

- `createOneRequest` helper extracted; the single `handleCreateRequest` still behaves identically (existing request tests stay green).
- `POST /requests/batch`: an `auto_approve` user → each item enqueued (album granularity) + `created` count correct; a `request`-only user → N pending, no enqueue; an already-open album in the batch → counted `skipped`, not re-created/re-enqueued; a mix → correct created/skipped split; an empty items list → `{created:0,skipped:0}`.
- FE Artist page: "Request all" gated on `can('request')`; filters to not-fully-owned entries; disclosure → confirm → POSTs `/requests/batch` with the right album items (source/id/name/artist/kind/trackCount); the success toast reflects auto vs pending; disabled when nothing to request.
- e2e (hermetic): on an artist page, "Request all" → batch POST asserted (kind:album items) → toast; the requested albums appear in `/requests`.
