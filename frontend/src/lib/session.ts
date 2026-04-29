// Session = the cookie this browser is signed in with.
//
// Browser sessions replaced the localStorage-bearer flow. The
// agentboard_session cookie (HttpOnly, set by /api/auth/login) is
// what authenticates every API call from the SPA; agentboard_csrf
// (readable here) is copied into X-CSRF-Token on every state-
// changing call so a cross-origin form post can't ride the cookie.
//
// PATs (`ab_*`) still exist as a separate non-browser credential,
// minted from /tokens. The SPA never uses one — it always speaks
// to the API as the cookie-authenticated user.

export const LOGIN_PATH = '/login'

// publicMode is set by SessionGate when an anonymous visitor
// lands on a route that matches the project's public.paths
// config. When on, apiFetch stops redirecting 401s to /login —
// the visitor is expected to see a degraded (but functional)
// view and explicitly click "Sign in" if they want more.
let publicMode = false

export function setPublicMode(on: boolean) {
  publicMode = on
}

export function isPublicMode(): boolean {
  return publicMode
}

export interface SessionUser {
  username: string
  display_name?: string
  kind: 'admin' | 'member' | 'bot'
  avatar_color?: string
}

// readCookie returns the value of a cookie set on the document, or
// null. The CSRF cookie is the only one the SPA needs to read; the
// session cookie is HttpOnly and so invisible from JS.
function readCookie(name: string): string | null {
  if (typeof document === 'undefined') return null
  const want = name + '='
  for (const part of document.cookie.split(';')) {
    const trimmed = part.trim()
    if (trimmed.startsWith(want)) {
      return decodeURIComponent(trimmed.substring(want.length))
    }
  }
  return null
}

// redirectToLogin hard-navigates to /login and remembers the current
// path so we can bounce the user back after they sign in. Uses
// window.location.assign so it works from anywhere (hooks, event
// handlers, the apiFetch 401 branch) without a component context.
export function redirectToLogin(reason?: 'unauthorized' | 'missing') {
  if (typeof window === 'undefined') return
  const cur = window.location.pathname + window.location.search
  const params = new URLSearchParams()
  if (cur && cur !== LOGIN_PATH) params.set('next', cur)
  if (reason) params.set('reason', reason)
  const qs = params.toString()
  window.location.assign(qs ? `${LOGIN_PATH}?${qs}` : LOGIN_PATH)
}

// isSameOrigin returns true when url targets this page's origin.
// Relative paths count as same-origin. We only attach the CSRF
// header for same-origin requests so user-provided URLs (e.g.
// ApiList components pointing at third-party JSON) can't leak it.
function isSameOrigin(url: string): boolean {
  if (!url) return true
  if (url.startsWith('/') && !url.startsWith('//')) return true
  try {
    const parsed = new URL(url, window.location.origin)
    return parsed.origin === window.location.origin
  } catch {
    return false
  }
}

// apiFetch wraps fetch with cookie-aware credentials + CSRF +
// centralized 401 handling.
//
//   - credentials: 'include' so the agentboard_session cookie rides
//     every same-origin request.
//   - For non-GET/HEAD/OPTIONS calls we attach X-CSRF-Token from
//     the agentboard_csrf cookie. The server enforces equality; a
//     missing or mismatched value 403s with code=CSRF_REQUIRED.
//   - On 401, redirect to /login (unless `skipAuth` or publicMode).
//
// `skipAuth` exists for /api/auth/login and the SessionGate's
// /api/auth/me probe — both must be free to receive a 401 without
// triggering the redirect.
export async function apiFetch(
  input: RequestInfo | URL,
  init: RequestInit & { skipAuth?: boolean } = {},
): Promise<Response> {
  const { skipAuth, headers, credentials, ...rest } = init
  const merged = new Headers(headers || {})
  const urlStr = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const sameOrigin = isSameOrigin(urlStr)
  const method = (rest.method || 'GET').toUpperCase()
  if (sameOrigin && method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
    const csrf = readCookie('agentboard_csrf')
    if (csrf && !merged.has('X-CSRF-Token')) {
      merged.set('X-CSRF-Token', csrf)
    }
  }
  const res = await fetch(input, {
    ...rest,
    headers: merged,
    credentials: credentials ?? 'include',
  })
  if (res.status === 401 && !skipAuth && sameOrigin && !publicMode) {
    redirectToLogin('unauthorized')
  }
  return res
}

// sseURL returns the path unchanged. EventSource sends cookies on
// same-origin requests automatically, so the cookie-based session
// authenticates SSE streams without any URL-side plumbing.
export function sseURL(path: string): string {
  return path
}

export interface SetupStatus {
  initialized: boolean
  invite_url?: string
}

// fetchSetupStatus asks the server whether the board has been
// claimed and, when unclaimed, whether a bootstrap invitation is
// active. Open endpoint — no auth needed. Returns a permissive
// default on network error so the UI falls through to the sign-
// in form rather than showing "first setup" on a flaky connection.
export async function fetchSetupStatus(): Promise<SetupStatus> {
  try {
    const res = await fetch('/api/setup/status')
    if (!res.ok) return { initialized: true }
    return (await res.json()) as SetupStatus
  } catch {
    return { initialized: true }
  }
}

// fetchSessionUser probes /api/auth/me. Returns null on 401 (not
// signed in). The SPA's SessionGate uses this to decide whether
// to render the protected shell or bounce to /login.
export async function fetchSessionUser(): Promise<SessionUser | null> {
  try {
    const res = await apiFetch('/api/auth/me', { skipAuth: true })
    if (res.status === 200) {
      const body = (await res.json()) as { user: SessionUser }
      return body.user
    }
  } catch {
    // network error — treat as not signed in
  }
  return null
}

// signInWithPassword posts to /api/auth/login. The response sets
// the agentboard_session + agentboard_csrf cookies; on success
// the caller can navigate freely. Returns the user shape on 200,
// or throws an Error on any non-200 with the server's message.
export async function signInWithPassword(
  username: string,
  password: string,
): Promise<SessionUser> {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ username, password }),
  })
  if (res.ok) {
    const body = (await res.json()) as { user: SessionUser }
    return body.user
  }
  let msg = 'Sign-in failed.'
  try {
    const body = await res.json()
    msg = body.error || body.message || msg
  } catch {
    // ignore body decode failure
  }
  throw new Error(msg)
}

// signOut posts to /api/auth/logout. Idempotent — the server
// clears cookies and revokes the session row whether or not it
// was already gone.
export async function signOut(): Promise<void> {
  try {
    await apiFetch('/api/auth/logout', { method: 'POST', skipAuth: true })
  } catch {
    // best-effort
  }
}
