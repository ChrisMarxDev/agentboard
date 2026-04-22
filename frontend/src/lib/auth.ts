// Admin-endpoint helpers. The token is shared with the rest of the SPA
// (see lib/session.ts) — admin vs agent capability comes from the token's
// `kind`, not from a separate store.

import { apiFetch } from './session'

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

export interface Me {
  username: string
  display_name?: string
  kind: 'admin' | 'agent'
  avatar_color?: string
}

export async function fetchMe(): Promise<Me> {
  return json<Me>(await apiFetch('/api/admin/me'))
}

// -------- users --------

export type Kind = 'admin' | 'agent'
export type AccessMode = 'allow_all' | 'restrict_to_list'

export interface Rule {
  action: 'allow' | 'deny'
  pattern: string
  methods: string[]
}

export interface UserToken {
  id: string
  username: string
  label?: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface User {
  username: string
  display_name?: string
  kind: Kind
  avatar_color?: string
  access_mode: AccessMode
  rules: Rule[]
  created_at: string
  created_by?: string
  deactivated_at?: string
  tokens?: UserToken[]
}

export async function listUsers(): Promise<User[]> {
  const data = await json<{ users: User[] }>(await apiFetch('/api/admin/users'))
  return data.users ?? []
}

export interface CreatedToken {
  username: string
  token_id: string
  label?: string
  token: string
}

export async function createUser(params: {
  username: string
  display_name?: string
  kind: Kind
  access_mode: AccessMode
  rules: Rule[]
  initial_token_label?: string
}): Promise<CreatedToken> {
  return json<CreatedToken>(await apiFetch('/api/admin/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  }))
}

// updateUser does NOT accept username. Usernames are immutable; to rename,
// run `agentboard admin rename-user <old> <new>` on the host.
export async function updateUser(
  username: string,
  patch: {
    display_name?: string
    access_mode?: AccessMode
    rules?: Rule[]
  },
): Promise<User> {
  return json<User>(await apiFetch(`/api/admin/users/${encodeURIComponent(username)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  }))
}

export async function deactivateUser(username: string): Promise<void> {
  const res = await apiFetch(`/api/admin/users/${encodeURIComponent(username)}/deactivate`, { method: 'POST' })
  if (!res.ok) throw await parseError(res)
}

// -------- tokens --------

export async function createTokenForUser(username: string, label?: string): Promise<CreatedToken> {
  return json<CreatedToken>(await apiFetch(
    `/api/admin/users/${encodeURIComponent(username)}/tokens`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ label: label ?? '' }),
    },
  ))
}

export async function rotateToken(username: string, tokenId: string): Promise<CreatedToken> {
  return json<CreatedToken>(await apiFetch(
    `/api/admin/users/${encodeURIComponent(username)}/tokens/${encodeURIComponent(tokenId)}/rotate`,
    { method: 'POST' },
  ))
}

export async function revokeToken(username: string, tokenId: string): Promise<void> {
  const res = await apiFetch(
    `/api/admin/users/${encodeURIComponent(username)}/tokens/${encodeURIComponent(tokenId)}/revoke`,
    { method: 'POST' },
  )
  if (!res.ok) throw await parseError(res)
}
