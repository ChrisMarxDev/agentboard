// Admin-session helpers. The session cookie is HTTP-only and scoped to
// /api/admin, so the browser attaches it automatically on every admin
// fetch. We keep a CSRF token in memory (received on setup/login/me) and
// attach it to state-changing requests.
//
// This module intentionally does NOT store anything on disk. localStorage
// is reserved for agent tokens; the admin session lives entirely in the
// cookie + this in-memory CSRF token.

export interface Me {
  id: string
  name: string
  csrf_token: string
}

let csrfToken: string | null = null

export function setCSRF(t: string) {
  csrfToken = t
}

export function getCSRF(): string | null {
  return csrfToken
}

export function clearCSRF() {
  csrfToken = null
}

function headers(extra?: HeadersInit): HeadersInit {
  const base: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (csrfToken) base['X-CSRF-Token'] = csrfToken
  return { ...base, ...(extra as Record<string, string>) }
}

async function parseError(res: Response): Promise<Error> {
  try {
    const body = await res.json()
    return new Error(body.error || body.message || res.statusText)
  } catch {
    return new Error(`${res.status} ${res.statusText}`)
  }
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw await parseError(res)
  return (await res.json()) as T
}

// -------- session lifecycle --------

export async function fetchMe(): Promise<Me | null> {
  const res = await fetch('/api/admin/me', { credentials: 'same-origin' })
  if (res.status === 401) return null
  const me = await json<Me>(res)
  setCSRF(me.csrf_token)
  return me
}

export async function setup(code: string, name: string, password: string): Promise<Me> {
  const res = await fetch('/api/admin/setup', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify({ code, name, password }),
  })
  const me = await json<Me>(res)
  setCSRF(me.csrf_token)
  return me
}

export async function login(name: string, password: string): Promise<Me> {
  const res = await fetch('/api/admin/login', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify({ name, password }),
  })
  const me = await json<Me>(res)
  setCSRF(me.csrf_token)
  return me
}

export async function logout(): Promise<void> {
  await fetch('/api/admin/logout', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
  })
  clearCSRF()
}

export async function changePassword(current: string, next: string): Promise<Me> {
  const res = await fetch('/api/admin/password', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify({ current, new: next }),
  })
  const me = await json<Me>(res)
  setCSRF(me.csrf_token)
  return me
}

// -------- identities --------

export type Kind = 'admin' | 'agent'
export type AccessMode = 'allow_all' | 'restrict_to_list'

export interface Rule {
  action: 'allow' | 'deny'
  pattern: string
  methods: string[]
}

export interface Identity {
  id: string
  name: string
  kind: Kind
  access_mode: AccessMode
  rules: Rule[]
  created_at: string
  created_by?: string
  last_used_at?: string
  revoked_at?: string
}

export async function listIdentities(): Promise<Identity[]> {
  const res = await fetch('/api/admin/identities', { credentials: 'same-origin' })
  const data = await json<{ identities: Identity[] }>(res)
  return data.identities ?? []
}

export interface CreatedAgent {
  id: string
  name: string
  token: string
}

export async function createAgent(
  name: string,
  accessMode: AccessMode,
  rules: Rule[],
): Promise<CreatedAgent> {
  const res = await fetch('/api/admin/identities', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify({
      name,
      kind: 'agent',
      access_mode: accessMode,
      rules,
    }),
  })
  return json<CreatedAgent>(res)
}

export async function createAdmin(name: string, password: string): Promise<Identity> {
  const res = await fetch('/api/admin/identities', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify({ name, kind: 'admin', password }),
  })
  return json<Identity>(res)
}

export async function updateIdentity(
  id: string,
  patch: { name?: string; access_mode?: AccessMode; rules?: Rule[] },
): Promise<Identity> {
  const res = await fetch(`/api/admin/identities/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify(patch),
  })
  return json<Identity>(res)
}

export async function rotateAgent(id: string): Promise<CreatedAgent> {
  const res = await fetch(
    `/api/admin/identities/${encodeURIComponent(id)}/rotate`,
    {
      method: 'POST',
      credentials: 'same-origin',
      headers: headers(),
    },
  )
  return json<CreatedAgent>(res)
}

export async function revokeIdentity(id: string): Promise<void> {
  const res = await fetch(
    `/api/admin/identities/${encodeURIComponent(id)}/revoke`,
    {
      method: 'POST',
      credentials: 'same-origin',
      headers: headers(),
    },
  )
  if (!res.ok) throw await parseError(res)
}

// -------- bootstrap codes --------

export interface BootstrapCode {
  id: string
  fingerprint: string
  created_at: string
  expires_at: string
  note?: string
}

export interface CreatedBootstrapCode {
  id: string
  code: string
  expires_at: string
  note?: string
}

export async function createBootstrapCode(
  ttlHours: number,
  note?: string,
): Promise<CreatedBootstrapCode> {
  const res = await fetch('/api/admin/bootstrap-codes', {
    method: 'POST',
    credentials: 'same-origin',
    headers: headers(),
    body: JSON.stringify({ ttl_hours: ttlHours, note: note ?? '' }),
  })
  return json<CreatedBootstrapCode>(res)
}

export async function listBootstrapCodes(): Promise<BootstrapCode[]> {
  const res = await fetch('/api/admin/bootstrap-codes', {
    credentials: 'same-origin',
  })
  const data = await json<{ codes: BootstrapCode[] }>(res)
  return data.codes ?? []
}

export async function deleteBootstrapCode(id: string): Promise<void> {
  const res = await fetch(
    `/api/admin/bootstrap-codes/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: headers(),
    },
  )
  if (!res.ok) throw await parseError(res)
}
