// Session = the bearer token this browser is signed in with.
//
// One token per session — same one powers dashboards, data access, and the
// /admin page. The server decides what the token can do based on the
// user's kind + rules; the frontend just attaches it.

const STORAGE_KEY = 'agentboard:token'
export const LOGIN_PATH = '/login'

export interface SessionUser {
  username: string
  display_name?: string
  kind: 'admin' | 'agent'
  avatar_color?: string
}

export function getToken(): string | null {
  try {
    return window.localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

export function setToken(token: string) {
  window.localStorage.setItem(STORAGE_KEY, token)
}

export function clearToken() {
  window.localStorage.removeItem(STORAGE_KEY)
}

// redirectToLogin hard-navigates to /login and remembers the current path
// so we can bounce the user back after they sign in. Uses
// window.location.assign rather than react-router Navigate so it works
// from anywhere (hooks, event handlers, the apiFetch 401 branch) without
// a component context.
export function redirectToLogin(reason?: 'expired' | 'missing') {
  if (typeof window === 'undefined') return
  const cur = window.location.pathname + window.location.search
  const params = new URLSearchParams()
  if (cur && cur !== LOGIN_PATH) params.set('next', cur)
  if (reason) params.set('reason', reason)
  const qs = params.toString()
  window.location.assign(qs ? `${LOGIN_PATH}?${qs}` : LOGIN_PATH)
}

// apiFetch wraps fetch with the bearer header + centralized 401 handling.
//
//   - If a token is stored, it's attached as Authorization: Bearer.
//   - On a 401, the token is cleared and the user is redirected to /login
//     with a `reason=expired` marker. The rejected Promise still carries
//     the error so callers can short-circuit.
//   - 403 does NOT redirect — that means "you're signed in but can't touch
//     this". Callers decide how to render.
//
// The `skipAuth` option exists for /api/health and the login helpers that
// happen before a token exists.
// isSameOrigin returns true when url targets this page's origin. Relative
// paths count as same-origin. We only attach the bearer for same-origin
// requests so user-provided URLs (e.g. ApiList components pointing at
// third-party JSON) can't leak the token.
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

export async function apiFetch(input: RequestInfo | URL, init: RequestInit & { skipAuth?: boolean } = {}): Promise<Response> {
  const { skipAuth, headers, ...rest } = init
  const merged = new Headers(headers || {})
  const urlStr = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  if (!skipAuth && isSameOrigin(urlStr)) {
    const tok = getToken()
    if (tok && !merged.has('Authorization')) merged.set('Authorization', `Bearer ${tok}`)
  }
  const res = await fetch(input, { ...rest, headers: merged })
  if (res.status === 401 && !skipAuth && isSameOrigin(urlStr)) {
    // Token missing / invalid / revoked. Clear it so the login page shows
    // a fresh prompt rather than re-submitting the dead one, and bounce.
    clearToken()
    redirectToLogin('expired')
  }
  return res
}

// sseURL returns an EventSource URL with ?token= appended when a token is
// stored. EventSource can't set Authorization headers, so the ?token=
// query-param path (already supported by the middleware) is how SSE auths.
export function sseURL(path: string): string {
  const tok = getToken()
  if (!tok) return path
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}token=${encodeURIComponent(tok)}`
}
