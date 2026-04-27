// Auth API helpers. The token is shared with the rest of the SPA
// (see lib/session.ts) — admin vs member/bot capability comes from the
// user's `kind`, not from a separate store.

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

// Kind enumerates the three user kinds. Matches internal/auth.Kind.
//
// - admin  : manages users, invitations, teams, webhooks, page locks
// - member : normal human user; manages own tokens
// - bot    : shared puppet; any admin can mint/rotate tokens for it
export type Kind = 'admin' | 'member' | 'bot'

export interface Me {
  username: string
  display_name?: string
  kind: Kind
  avatar_color?: string
}

// Single source of truth for "who am I signed in as". Fetched by the
// shell's UserMenu and by any page that needs to branch on role.
export async function fetchMe(): Promise<Me> {
  return json<Me>(await apiFetch('/api/me'))
}

// -------- users --------

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
  created_by?: string
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

// -------- tokens (scoped: self-or-admin) --------

// All four token endpoints now live at /api/users/{username}/tokens/*
// with the ScopeSelfOrAdmin middleware. A member hitting these with
// their own username succeeds; with another user's username, 403.
// An admin succeeds against anyone.

export async function listTokensForUser(username: string): Promise<UserToken[]> {
  const data = await json<{ tokens: UserToken[] }>(
    await apiFetch(`/api/users/${encodeURIComponent(username)}/tokens`),
  )
  return data.tokens ?? []
}

export async function createTokenForUser(username: string, label?: string): Promise<CreatedToken> {
  return json<CreatedToken>(await apiFetch(
    `/api/users/${encodeURIComponent(username)}/tokens`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ label: label ?? '' }),
    },
  ))
}

export async function rotateToken(username: string, tokenId: string): Promise<CreatedToken> {
  return json<CreatedToken>(await apiFetch(
    `/api/users/${encodeURIComponent(username)}/tokens/${encodeURIComponent(tokenId)}/rotate`,
    { method: 'POST' },
  ))
}

export async function revokeToken(username: string, tokenId: string): Promise<void> {
  const res = await apiFetch(
    `/api/users/${encodeURIComponent(username)}/tokens/${encodeURIComponent(tokenId)}/revoke`,
    { method: 'POST' },
  )
  if (!res.ok) throw await parseError(res)
}

// -------- invitations --------

export interface Invitation {
  id: string
  role: Kind
  created_by: string
  created_at: string
  expires_at: string
  label?: string
  redeemed_at?: string
  redeemed_by?: string
  revoked_at?: string
  status: 'active' | 'redeemed' | 'expired' | 'revoked'
}

export async function listInvitations(): Promise<Invitation[]> {
  const data = await json<{ invitations: Invitation[] }>(
    await apiFetch('/api/admin/invitations'),
  )
  return data.invitations ?? []
}

export async function createInvitation(params: {
  role: Kind
  label?: string
  expires_in_days?: number
}): Promise<Invitation> {
  return json<Invitation>(await apiFetch('/api/admin/invitations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  }))
}

export async function revokeInvitation(id: string): Promise<void> {
  const res = await apiFetch(`/api/admin/invitations/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!res.ok) throw await parseError(res)
}

// Public — no auth required. Returns 404 if unusable (expired /
// redeemed / revoked / missing).
export interface PublicInvitationView {
  id: string
  role: Kind
  created_by: string
  label?: string
  expires_at: string
  bootstrap: boolean
}

export async function getInvitationPublic(id: string): Promise<PublicInvitationView> {
  const res = await fetch(`/api/invitations/${encodeURIComponent(id)}`)
  if (!res.ok) throw await parseError(res)
  return (await res.json()) as PublicInvitationView
}

export interface RedeemedInvitation {
  token: string
  token_id: string
  invitation_id: string
  role: Kind
  user: {
    username: string
    display_name?: string
    kind: Kind
    avatar_color?: string
  }
}

export async function redeemInvitation(
  id: string,
  username: string,
  displayName?: string,
): Promise<RedeemedInvitation> {
  const res = await fetch(`/api/invitations/${encodeURIComponent(id)}/redeem`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, display_name: displayName }),
  })
  if (!res.ok) throw await parseError(res)
  return (await res.json()) as RedeemedInvitation
}

// -------- page locks --------

export interface PageLock {
  path: string
  locked_by: string
  locked_at: string
  reason?: string
}

export async function listLocks(): Promise<PageLock[]> {
  const data = await json<{ locks: PageLock[] }>(await apiFetch('/api/locks'))
  return data.locks ?? []
}

export async function createLock(path: string, reason?: string): Promise<PageLock> {
  return json<PageLock>(await apiFetch('/api/locks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, reason }),
  }))
}

export async function deleteLock(path: string): Promise<void> {
  const res = await apiFetch(`/api/locks/${encodeURI(path)}`, { method: 'DELETE' })
  if (!res.ok) throw await parseError(res)
}
