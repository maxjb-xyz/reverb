# Reverb Auth/Login Fix Implementation Plan (Overhaul Phase 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make login work end-to-end over plain `http://` on a LAN by deriving the session cookie's `Secure` flag from the actual request scheme instead of `!dev`, and make the auth guard distinguish "not logged in" from "server unreachable".

**Architecture:** The session cookie is currently set `Secure: !dev` ([internal/api/middleware.go:46](../../../internal/api/middleware.go#L46)). On a non-dev build served over `http://<lan-ip>` the browser silently discards a `Secure` cookie, so the next authed request (`/me`) 401s and the SPA guard bounces back to Login/Setup forever. Fix the backend to set `Secure` from `r.TLS`/`X-Forwarded-Proto`, and tighten the frontend guard + login error copy so failures are honest and a transient server error no longer masquerades as "logged out".

**Tech Stack:** Go (chi, `net/http`, `httptest`) backend; React 19 + TypeScript + Vitest frontend.

## Global Constraints

- Backend tests live in package `api` under `internal/api/`; run with `go test ./internal/api/...`.
- Frontend pure logic is extracted into testable functions (no network, no render) and tested with Vitest under `web/`; run with `npx vitest run <file>`.
- TDD: write the failing test first, watch it fail, implement, watch it pass, commit. One logical change per commit (Conventional Commits).
- Every commit message ends with the trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Do NOT restyle Login/Setup here — that is Overhaul Phase 3 (shell). This phase is functional only; keep visual changes to the minimum needed for correct error copy.
- Branch is already `feat/ui-overhaul-spotify`. Stay on it.
- Spec: [docs/superpowers/specs/2026-06-21-reverb-ui-overhaul-design.md](../specs/2026-06-21-reverb-ui-overhaul-design.md) §11 Phase 1.

## File Structure

- `internal/api/middleware.go` — modify `setSessionCookie` to take `*http.Request` and derive `Secure`; add `cookieSecure` helper.
- `internal/api/handlers.go` — modify `issueSession`/`handleLogout` callers to pass `r`; drop now-unused `context` import.
- `internal/api/auth_cookie_test.go` — **new** Go test pinning cookie `Secure` to request scheme.
- `web/src/lib/api.ts` — add `ApiError` (carries `status`) and `loginErrorMessage` helper; throw `ApiError` on non-OK.
- `web/src/lib/session.ts` — extract pure `probeSession(get)` returning a `SessionKind`; refactor `useSessionStatus` to use it; add `error` to `SessionStatus`.
- `web/src/lib/session.test.ts` — **new** Vitest for `probeSession`.
- `web/src/lib/api.test.ts` — **new** Vitest for `loginErrorMessage`.
- `web/src/App.tsx` — add an `error` branch to the guard.
- `web/src/routes/Login.tsx` — use `loginErrorMessage` for specific copy.

---

### Task 1: Backend — session cookie `Secure` follows the request scheme

**Files:**
- Modify: `internal/api/middleware.go:35-49`
- Modify: `internal/api/handlers.go:60-61, 74, 77-85, 89` (callers + import)
- Test: `internal/api/auth_cookie_test.go` (create)

**Interfaces:**
- Produces: `func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string)` and `func cookieSecure(r *http.Request) bool`. `func (s *Server) issueSession(w http.ResponseWriter, r *http.Request)`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/auth_cookie_test.go`:

```go
package api

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sessionCookieFromSetup completes first-run setup on a fresh server and returns
// the session cookie it set, after applying mutate to the setup request.
func sessionCookieFromSetup(t *testing.T, mutate func(*http.Request)) *http.Cookie {
	t.Helper()
	srv := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"password":"pw"}`))
	if mutate != nil {
		mutate(req)
	}
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup/admin = %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func TestSessionCookieInsecureOverPlainHTTP(t *testing.T) {
	c := sessionCookieFromSetup(t, nil)
	if c.Secure {
		t.Fatal("session cookie must NOT be Secure over plain http (the browser drops it on a LAN, causing the login loop)")
	}
	if !c.HttpOnly {
		t.Fatal("session cookie must stay HttpOnly")
	}
}

func TestSessionCookieSecureBehindHTTPSProxy(t *testing.T) {
	c := sessionCookieFromSetup(t, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") })
	if !c.Secure {
		t.Fatal("session cookie must be Secure when X-Forwarded-Proto=https")
	}
}

func TestSessionCookieSecureOverDirectTLS(t *testing.T) {
	c := sessionCookieFromSetup(t, func(r *http.Request) { r.TLS = &tls.ConnectionState{} })
	if !c.Secure {
		t.Fatal("session cookie must be Secure over a direct TLS connection")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestSessionCookie -v`
Expected: `TestSessionCookieInsecureOverPlainHTTP` FAILS ("must NOT be Secure over plain http") because the cookie is currently `Secure: !dev` → `true`. The other two pass already.

- [ ] **Step 3: Implement — derive Secure from the request in `middleware.go`**

Replace the body of `internal/api/middleware.go` from line 1 import block and `setSessionCookie` so the file reads:

```go
package api

import (
	"net/http"
	"strings"
)

const sessionCookie = "reverb_session"

func (s *Server) tokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	const p = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if disabled, _ := s.deps.Auth.IsAuthDisabled(r.Context()); disabled {
			next.ServeHTTP(w, r)
			return
		}
		ok, _ := s.deps.Auth.ValidateToken(r.Context(), s.tokenFromRequest(r))
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cookieSecure reports whether the session cookie should carry the Secure flag,
// based on the real request scheme. Direct TLS or an https-terminating reverse
// proxy (X-Forwarded-Proto: https) → Secure. Plain http (LAN, no TLS) → not Secure,
// otherwise the browser silently drops the cookie and every authed request 401s.
func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	maxAge := 30 * 24 * 3600
	if token == "" {
		maxAge = -1 // delete the cookie
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   maxAge,
	})
}
```

- [ ] **Step 4: Implement — update callers in `handlers.go`**

In `internal/api/handlers.go`: remove `"context"` from the import block (it becomes unused), and change the three call sites:

Change `issueSession` (lines ~77-85) to take the request:

```go
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	tok, err := s.deps.Auth.CreateSession(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	s.setSessionCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

In `handleSetupAdmin` change `s.issueSession(w, r.Context())` → `s.issueSession(w, r)`.
In `handleLogin` change `s.issueSession(w, r.Context())` → `s.issueSession(w, r)`.
In `handleLogout` change `s.setSessionCookie(w, "", s.deps.Dev)` → `s.setSessionCookie(w, r, "")`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/api/ -run TestSessionCookie -v`
Expected: all three PASS.

- [ ] **Step 6: Run the full api package to check nothing regressed**

Run: `go test ./internal/api/...`
Expected: PASS (including `TestSetupThenProtectedAccess`, which still asserts the cookie is HttpOnly).

- [ ] **Step 7: Commit**

```bash
git add internal/api/middleware.go internal/api/handlers.go internal/api/auth_cookie_test.go
git commit -m "fix(auth): set session cookie Secure from request scheme, not !dev"
```

---

### Task 2: Frontend — typed `ApiError`, `loginErrorMessage`, and pure `probeSession`

**Files:**
- Modify: `web/src/lib/api.ts:1-20`
- Modify: `web/src/lib/session.ts:1-32`
- Test: `web/src/lib/api.test.ts` (create), `web/src/lib/session.test.ts` (create)

**Interfaces:**
- Produces (api.ts): `class ApiError extends Error { status: number }`; `function loginErrorMessage(e: unknown): string`.
- Produces (session.ts): `type SessionKind = 'setup' | 'authenticated' | 'unauthenticated' | 'error'`; `function probeSession(get: <T>(p: string) => Promise<T>): Promise<SessionKind>`; `interface SessionStatus { loading; setupRequired; authenticated; error }`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/api.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { ApiError, loginErrorMessage } from './api'

describe('loginErrorMessage', () => {
  it('maps a 401 to an incorrect-password message', () => {
    expect(loginErrorMessage(new ApiError('POST', '/auth/login', 401))).toBe('Incorrect password')
  })
  it('maps a 500 to a server-unreachable message', () => {
    expect(loginErrorMessage(new ApiError('POST', '/auth/login', 500))).toMatch(/server/i)
  })
  it('maps a thrown network error to a server-unreachable message', () => {
    expect(loginErrorMessage(new TypeError('Failed to fetch'))).toMatch(/server/i)
  })
})
```

Create `web/src/lib/session.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { probeSession } from './session'
import { ApiError } from './api'

function fakeGet(ok: Record<string, unknown>, errs: Record<string, Error> = {}) {
  return async <T>(p: string): Promise<T> => {
    if (errs[p]) throw errs[p]
    return ok[p] as T
  }
}

describe('probeSession', () => {
  it('returns "setup" when setup is required', async () => {
    expect(await probeSession(fakeGet({ '/setup/status': { setupRequired: true } }))).toBe('setup')
  })
  it('returns "authenticated" when /me succeeds', async () => {
    const get = fakeGet({ '/setup/status': { setupRequired: false }, '/me': { authenticated: true } })
    expect(await probeSession(get)).toBe('authenticated')
  })
  it('returns "unauthenticated" when /me is 401', async () => {
    const get = fakeGet({ '/setup/status': { setupRequired: false } }, { '/me': new ApiError('GET', '/me', 401) })
    expect(await probeSession(get)).toBe('unauthenticated')
  })
  it('returns "error" when /me fails with a server error', async () => {
    const get = fakeGet({ '/setup/status': { setupRequired: false } }, { '/me': new ApiError('GET', '/me', 500) })
    expect(await probeSession(get)).toBe('error')
  })
  it('returns "error" when the server is unreachable', async () => {
    expect(await probeSession(fakeGet({}, { '/setup/status': new TypeError('Failed to fetch') }))).toBe('error')
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/api.test.ts src/lib/session.test.ts`
Expected: FAIL — `loginErrorMessage`/`probeSession` are not exported yet (import/resolve errors).

- [ ] **Step 3: Implement `web/src/lib/api.ts`**

Replace the file with:

```ts
const BASE = '/api/v1'

export class ApiError extends Error {
  status: number
  constructor(method: string, path: string, status: number) {
    super(`${method} ${path} -> ${status}`)
    this.name = 'ApiError'
    this.status = status
  }
}

/** Human, specific copy for a failed login attempt. */
export function loginErrorMessage(e: unknown): string {
  if (e instanceof ApiError && e.status === 401) return 'Incorrect password'
  return 'Can’t reach the server — try again'
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new ApiError(method, path, res.status)
  const text = await res.text()
  return (text ? JSON.parse(text) : null) as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}
```

- [ ] **Step 4: Implement `web/src/lib/session.ts`**

Replace the file with:

```ts
import { useEffect, useState } from 'react'
import { api, ApiError } from './api'

export type SessionKind = 'setup' | 'authenticated' | 'unauthenticated' | 'error'

export interface SessionStatus {
  loading: boolean
  setupRequired: boolean
  authenticated: boolean
  error: boolean
}

// probeSession classifies the session using an injected `get`, so it is unit-testable
// without a network or a rendered component. A 401 on /me means "not logged in";
// anything else (5xx, network failure) means "can't determine" → error (do NOT treat
// a transient server error as logged-out and bounce the user to Login).
export async function probeSession(get: <T>(p: string) => Promise<T>): Promise<SessionKind> {
  let setup: { setupRequired: boolean }
  try {
    setup = await get<{ setupRequired: boolean }>('/setup/status')
  } catch {
    return 'error'
  }
  if (setup.setupRequired) return 'setup'
  try {
    await get('/me')
    return 'authenticated'
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return 'unauthenticated'
    return 'error'
  }
}

export function useSessionStatus(): SessionStatus {
  const [s, setS] = useState<SessionStatus>({
    loading: true,
    setupRequired: false,
    authenticated: false,
    error: false,
  })
  useEffect(() => {
    ;(async () => {
      const kind = await probeSession(api.get)
      setS({
        loading: false,
        setupRequired: kind === 'setup',
        authenticated: kind === 'authenticated',
        error: kind === 'error',
      })
    })()
  }, [])
  return s
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/api.test.ts src/lib/session.test.ts`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/api.ts web/src/lib/session.ts web/src/lib/api.test.ts web/src/lib/session.test.ts
git commit -m "feat(web): typed ApiError + pure probeSession that distinguishes 401 from server errors"
```

---

### Task 3: Frontend — wire the guard error state and specific login copy

**Files:**
- Modify: `web/src/App.tsx:17-34`
- Modify: `web/src/routes/Login.tsx:3, 9-19`

**Interfaces:**
- Consumes: `SessionStatus.error` (Task 2), `loginErrorMessage` + `ApiError` (Task 2).

- [ ] **Step 1: Add the error branch to the guard in `web/src/App.tsx`**

Replace the `Routed` function (lines 17-34) with:

```tsx
function Routed() {
  const s = useSessionStatus()
  if (s.loading) return <div className="p-6 text-neutral-500">Loading…</div>
  if (s.error)
    return (
      <div className="p-6 text-neutral-400">
        Can’t reach the Reverb server.{' '}
        <button onClick={() => window.location.reload()} className="underline">
          Retry
        </button>
      </div>
    )
  if (s.setupRequired) return <Setup />
  if (!s.authenticated) return <Login />
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/album/:id" element={<Album />} />
        <Route path="/artist/:id" element={<Artist />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/search" replace />} />
      </Route>
    </Routes>
  )
}
```

- [ ] **Step 2: Use specific error copy in `web/src/routes/Login.tsx`**

Change the import on line 3 from `import { api } from '../lib/api'` to:

```tsx
import { api, loginErrorMessage } from '../lib/api'
```

Replace the `submit` handler (lines 9-19) with:

```tsx
  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    try {
      await api.post('/auth/login', { password: pw })
      window.location.reload()
    } catch (err) {
      setErr(loginErrorMessage(err))
    }
  }
```

- [ ] **Step 3: Typecheck and build the frontend**

Run: `cd web && npm run build`
Expected: `tsc -b && vite build` completes with no type errors (the new `error` field and `loginErrorMessage` resolve cleanly).

- [ ] **Step 4: Run the frontend test suite**

Run: `cd web && npx vitest run`
Expected: PASS (existing tests + the two new files).

- [ ] **Step 5: Commit**

```bash
git add web/src/App.tsx web/src/routes/Login.tsx
git commit -m "feat(web): show a server-unreachable state and specific login error copy"
```

---

### Task 4: End-to-end verification (the actual blocker)

**Files:** none (verification only).

**Interfaces:** none.

This proves the regression is gone at runtime, beyond the unit tests. The deterministic check is host-independent because it reads the raw `Set-Cookie` header.

- [ ] **Step 1: Start the backend over plain http**

Run (in one terminal): `go run ./cmd/reverb`
Expected: it logs listening on `:8090`. (No library needed for the auth check; uses the default SQLite DB.)

- [ ] **Step 2: Confirm the cookie is NOT Secure over http**

Run: `curl -i -s -X POST http://localhost:8090/api/v1/setup/admin -H 'Content-Type: application/json' -d '{"password":"testpw"}' | grep -i 'set-cookie'`
Expected: a `Set-Cookie: reverb_session=...` line containing `HttpOnly` and `SameSite=Lax` and **NOT** containing `Secure`. (Before the fix it contained `Secure`.)

- [ ] **Step 3: Confirm the cookie IS Secure behind an https proxy header**

Reset state first: `rm -f ./data/reverb.db` and restart `go run ./cmd/reverb` (setup can only run once). Then:
Run: `curl -i -s -X POST http://localhost:8090/api/v1/setup/admin -H 'Content-Type: application/json' -H 'X-Forwarded-Proto: https' -d '{"password":"testpw"}' | grep -i 'set-cookie'`
Expected: the `Set-Cookie` line now **contains** `Secure`.

- [ ] **Step 4: Browser sanity check (holistic)**

Build/run the app the way it is normally deployed (the prod-tagged binary or the Docker image, which embeds the SPA), then open it from another device or via the host's **LAN IP** over `http://` — e.g. `http://192.168.x.x:8090`. **Not** `http://localhost`: Chrome treats `localhost` as a secure context and stores `Secure` cookies there anyway, which would hide the bug.
Complete first-run setup (or log in), then refresh the page.
Expected: you land in the app and **stay** logged in across refresh — no bounce back to the setup/login screen, no "unauthorized".

- [ ] **Step 5: Clean up the throwaway DB**

Run: `rm -f ./data/reverb.db`
Expected: no error. (No commit — this task changed no tracked files.)

---

## Self-Review

**1. Spec coverage (§11 Phase 1):**
- "Reproduce + fix the cookie-Secure-over-http loop" → Task 1 (fix) + Task 4 (reproduce/verify). ✓
- "Derive Secure from request scheme / X-Forwarded-Proto" → Task 1 `cookieSecure`. ✓
- "Guard distinguishes unauthenticated from server error" → Task 2 `probeSession` + Task 3 App error branch. ✓
- "Honest, specific error copy (not bare 'unauthorized')" → Task 2 `loginErrorMessage` + Task 3 Login wiring. ✓
- "Verify login + first-run setup end-to-end" → Task 4. ✓
- "Do not restyle Login/Setup here" → Global Constraints; Task 3 keeps Login changes to the error handler only. ✓

**2. Placeholder scan:** No TBD/TODO/"add error handling"/"similar to". Every code step shows complete code. ✓

**3. Type consistency:** `setSessionCookie(w, r, token)` defined in Task 1 and called in Task 1 Step 4. `issueSession(w, r)` defined and called consistently. `ApiError.status`, `loginErrorMessage`, `probeSession`, and `SessionStatus.error` defined in Task 2 and consumed with identical names/signatures in Task 3. `SessionKind` literals (`'setup' | 'authenticated' | 'unauthenticated' | 'error'`) used consistently. ✓
