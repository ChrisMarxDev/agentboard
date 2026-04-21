import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  createAgent,
  createBootstrapCode,
  listBootstrapCodes,
  listIdentities,
  logout,
  rotateAgent,
  revokeIdentity,
  updateIdentity,
  type AccessMode,
  type BootstrapCode,
  type CreatedAgent,
  type CreatedBootstrapCode,
  type Identity,
  type Rule,
} from '../lib/auth'

// Admin page.
//
// Scope for the first cut:
//   - list identities (grouped by admin / agent)
//   - create new agents (name, access_mode, rules as JSON)
//   - rotate / revoke agents
//   - edit name + rules on existing agents
//   - mint bootstrap codes for onboarding another admin
//   - log out
//
// Intentionally NOT in v1 (all deferrable without schema changes):
//   - graceful rotation
//   - WebAuthn passkey enrollment
//   - per-session listing / remote session kill
//   - richer rules editor — JSON textarea is fine for now

const METHOD_OPTIONS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', '*'] as const

export default function Admin({ adminName }: { adminName: string }) {
  const navigate = useNavigate()
  const [identities, setIdentities] = useState<Identity[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [fresh, setFresh] = useState<CreatedAgent | null>(null)
  const [freshCode, setFreshCode] = useState<CreatedBootstrapCode | null>(null)
  const [codes, setCodes] = useState<BootstrapCode[]>([])

  const refresh = useCallback(async () => {
    try {
      const [i, c] = await Promise.all([listIdentities(), listBootstrapCodes()])
      setIdentities(i)
      setCodes(c)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  async function onLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-8 p-6">
      <header className="flex items-baseline justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Auth</h1>
          <p className="text-sm opacity-60">
            Logged in as <b>{adminName}</b>. Only the admin realm can manage
            identities — agents authenticate with tokens against data endpoints.
          </p>
        </div>
        <button
          onClick={onLogout}
          className="rounded border border-black/15 px-3 py-1 text-sm dark:border-white/15"
        >
          Log out
        </button>
      </header>

      {error && (
        <div className="rounded border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      )}

      {fresh && (
        <OneTimeTokenBanner
          created={fresh}
          onDismiss={() => setFresh(null)}
        />
      )}
      {freshCode && (
        <OneTimeCodeBanner
          created={freshCode}
          onDismiss={() => setFreshCode(null)}
        />
      )}

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Identities</h2>
        {identities === null ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : (
          <IdentitiesTable
            identities={identities}
            onChanged={refresh}
            onRotated={setFresh}
            onError={(e) => setError(e)}
          />
        )}
        <CreateAgentForm
          onCreated={(a) => {
            setFresh(a)
            void refresh()
          }}
          onError={(e) => setError(e)}
        />
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Bootstrap codes</h2>
        <p className="text-sm opacity-60">
          Codes are single-use. Hand one to another human so they can claim a
          second admin identity via <code>/setup</code>.
        </p>
        <BootstrapCodeList codes={codes} onChanged={refresh} onError={setError} />
        <CreateBootstrapCodeForm
          onCreated={(c) => {
            setFreshCode(c)
            void refresh()
          }}
          onError={setError}
        />
      </section>
    </div>
  )
}

// ---------- identities table ----------

function IdentitiesTable({
  identities,
  onChanged,
  onRotated,
  onError,
}: {
  identities: Identity[]
  onChanged: () => void
  onRotated: (c: CreatedAgent) => void
  onError: (msg: string) => void
}) {
  return (
    <div className="overflow-x-auto rounded border border-black/10 dark:border-white/10">
      <table className="w-full text-sm">
        <thead className="bg-black/5 text-left text-xs uppercase tracking-wide opacity-70 dark:bg-white/5">
          <tr>
            <th className="px-3 py-2">Name</th>
            <th className="px-3 py-2">Kind</th>
            <th className="px-3 py-2">Mode</th>
            <th className="px-3 py-2">Last used</th>
            <th className="px-3 py-2">Rules</th>
            <th className="px-3 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {identities.map((id) => (
            <IdentityRow
              key={id.id}
              identity={id}
              onChanged={onChanged}
              onRotated={onRotated}
              onError={onError}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function IdentityRow({
  identity,
  onChanged,
  onRotated,
  onError,
}: {
  identity: Identity
  onChanged: () => void
  onRotated: (c: CreatedAgent) => void
  onError: (msg: string) => void
}) {
  const [editing, setEditing] = useState(false)

  if (editing) {
    return (
      <IdentityEditRow
        identity={identity}
        onDone={() => {
          setEditing(false)
          onChanged()
        }}
        onCancel={() => setEditing(false)}
        onError={onError}
      />
    )
  }

  async function onRotate() {
    try {
      const rotated = await rotateAgent(identity.id)
      onRotated(rotated)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  async function onRevoke() {
    if (!window.confirm(`Revoke "${identity.name}"? Its token stops working immediately.`)) return
    try {
      await revokeIdentity(identity.id)
      onChanged()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <tr className={identity.revoked_at ? 'opacity-50' : ''}>
      <td className="px-3 py-2 font-medium">{identity.name}</td>
      <td className="px-3 py-2">{identity.kind}</td>
      <td className="px-3 py-2">{identity.access_mode}</td>
      <td className="px-3 py-2 text-xs opacity-70">
        {identity.last_used_at ? new Date(identity.last_used_at).toLocaleString() : '—'}
      </td>
      <td className="px-3 py-2 text-xs opacity-70">
        {identity.rules.length === 0 ? '—' : `${identity.rules.length} rule${identity.rules.length === 1 ? '' : 's'}`}
      </td>
      <td className="px-3 py-2 text-right">
        {!identity.revoked_at && identity.kind === 'agent' && (
          <div className="flex justify-end gap-2">
            <button
              onClick={() => setEditing(true)}
              className="rounded border border-black/15 px-2 py-1 text-xs dark:border-white/15"
            >
              Edit
            </button>
            <button
              onClick={onRotate}
              className="rounded border border-black/15 px-2 py-1 text-xs dark:border-white/15"
            >
              Rotate
            </button>
            <button
              onClick={onRevoke}
              className="rounded border border-red-500/40 px-2 py-1 text-xs text-red-700 dark:text-red-300"
            >
              Revoke
            </button>
          </div>
        )}
      </td>
    </tr>
  )
}

function IdentityEditRow({
  identity,
  onDone,
  onCancel,
  onError,
}: {
  identity: Identity
  onDone: () => void
  onCancel: () => void
  onError: (msg: string) => void
}) {
  const [name, setName] = useState(identity.name)
  const [mode, setMode] = useState<AccessMode>(identity.access_mode)
  const [rulesText, setRulesText] = useState(JSON.stringify(identity.rules, null, 2))

  async function save() {
    let rules: Rule[]
    try {
      rules = JSON.parse(rulesText) as Rule[]
      if (!Array.isArray(rules)) throw new Error('rules must be an array')
    } catch (err) {
      onError(`Rules JSON: ${err instanceof Error ? err.message : String(err)}`)
      return
    }
    try {
      await updateIdentity(identity.id, { name, access_mode: mode, rules })
      onDone()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <tr className="bg-black/5 dark:bg-white/5">
      <td colSpan={6} className="px-3 py-3">
        <div className="flex flex-col gap-2 text-sm">
          <div className="flex items-center gap-2">
            <label className="flex items-center gap-2">
              Name
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
              />
            </label>
            <label className="flex items-center gap-2">
              Mode
              <select
                value={mode}
                onChange={(e) => setMode(e.target.value as AccessMode)}
                className="rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
              >
                <option value="allow_all">allow_all (blocklist)</option>
                <option value="restrict_to_list">restrict_to_list (allowlist-only)</option>
              </select>
            </label>
          </div>
          <label className="flex flex-col gap-1">
            <span className="opacity-70 text-xs">Rules (JSON array)</span>
            <textarea
              rows={6}
              value={rulesText}
              onChange={(e) => setRulesText(e.target.value)}
              className="rounded border border-black/15 bg-white px-2 py-1 font-mono text-xs dark:border-white/15 dark:bg-black"
            />
          </label>
          <div className="flex justify-end gap-2">
            <button
              onClick={onCancel}
              className="rounded border border-black/15 px-3 py-1 text-xs dark:border-white/15"
            >
              Cancel
            </button>
            <button
              onClick={save}
              className="rounded bg-black px-3 py-1 text-xs text-white dark:bg-white dark:text-black"
            >
              Save
            </button>
          </div>
        </div>
      </td>
    </tr>
  )
}

// ---------- create agent ----------

function CreateAgentForm({
  onCreated,
  onError,
}: {
  onCreated: (a: CreatedAgent) => void
  onError: (msg: string) => void
}) {
  const [name, setName] = useState('')
  const [mode, setMode] = useState<AccessMode>('allow_all')
  const [template, setTemplate] = useState<'full' | 'viewer' | 'custom'>('full')
  const [rulesText, setRulesText] = useState('[]')
  const [busy, setBusy] = useState(false)

  function applyTemplate(t: 'full' | 'viewer' | 'custom') {
    setTemplate(t)
    if (t === 'full') {
      setMode('allow_all')
      setRulesText('[]')
    } else if (t === 'viewer') {
      setMode('restrict_to_list')
      setRulesText(
        JSON.stringify(
          [
            { action: 'allow', pattern: '/api/data/**', methods: ['GET'] },
            { action: 'allow', pattern: '/api/content/**', methods: ['GET'] },
            { action: 'allow', pattern: '/api/files/**', methods: ['GET'] },
            { action: 'allow', pattern: '/api/events', methods: ['GET'] },
          ] satisfies Rule[],
          null,
          2,
        ),
      )
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const rules: Rule[] = JSON.parse(rulesText) as Rule[]
      const created = await createAgent(name.trim(), mode, rules)
      setName('')
      applyTemplate('full')
      onCreated(created)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded border border-black/10 p-4 dark:border-white/10"
    >
      <h3 className="text-sm font-semibold">Create agent identity</h3>
      <div className="flex flex-wrap gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. ci-bot or alice-laptop"
            className="rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Template</span>
          <select
            value={template}
            onChange={(e) => applyTemplate(e.target.value as 'full' | 'viewer' | 'custom')}
            className="rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
          >
            <option value="full">Full access</option>
            <option value="viewer">Read-only viewer</option>
            <option value="custom">Custom</option>
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Mode</span>
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as AccessMode)}
            className="rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
          >
            <option value="allow_all">allow_all</option>
            <option value="restrict_to_list">restrict_to_list</option>
          </select>
        </label>
      </div>
      <label className="flex flex-col gap-1 text-sm">
        <span className="opacity-70">Rules (JSON array, methods: {METHOD_OPTIONS.join(', ')})</span>
        <textarea
          rows={6}
          value={rulesText}
          onChange={(e) => setRulesText(e.target.value)}
          className="rounded border border-black/15 bg-white px-2 py-1 font-mono text-xs dark:border-white/15 dark:bg-black"
        />
      </label>
      <div className="flex justify-end">
        <button
          type="submit"
          disabled={busy}
          className="rounded bg-black px-3 py-1.5 text-sm text-white disabled:opacity-60 dark:bg-white dark:text-black"
        >
          {busy ? 'Creating…' : 'Create + show token'}
        </button>
      </div>
    </form>
  )
}

// ---------- bootstrap codes ----------

function BootstrapCodeList({
  codes,
  onChanged: _onChanged,
  onError: _onError,
}: {
  codes: BootstrapCode[]
  onChanged: () => void
  onError: (msg: string) => void
}) {
  if (codes.length === 0) {
    return <div className="text-sm opacity-60">No outstanding codes.</div>
  }
  return (
    <div className="rounded border border-black/10 text-sm dark:border-white/10">
      {codes.map((c) => (
        <div
          key={c.id}
          className="flex items-center justify-between border-b border-black/5 px-3 py-2 last:border-none dark:border-white/5"
        >
          <div>
            <div className="font-mono text-xs opacity-80">{c.fingerprint}…</div>
            {c.note && <div className="text-xs opacity-60">{c.note}</div>}
          </div>
          <div className="text-xs opacity-60">
            expires {new Date(c.expires_at).toLocaleString()}
          </div>
        </div>
      ))}
    </div>
  )
}

function CreateBootstrapCodeForm({
  onCreated,
  onError,
}: {
  onCreated: (c: CreatedBootstrapCode) => void
  onError: (msg: string) => void
}) {
  const [ttl, setTtl] = useState(24)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const c = await createBootstrapCode(ttl, note)
      setNote('')
      onCreated(c)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-wrap items-end gap-3 rounded border border-black/10 p-3 dark:border-white/10"
    >
      <label className="flex flex-col gap-1 text-sm">
        <span className="opacity-70">TTL (hours)</span>
        <input
          type="number"
          min={1}
          max={720}
          value={ttl}
          onChange={(e) => setTtl(Number(e.target.value))}
          className="w-24 rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="opacity-70">Note (optional)</span>
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="e.g. onboarding bob"
          className="rounded border border-black/15 bg-white px-2 py-1 dark:border-white/15 dark:bg-black"
        />
      </label>
      <button
        type="submit"
        disabled={busy}
        className="rounded bg-black px-3 py-1.5 text-sm text-white disabled:opacity-60 dark:bg-white dark:text-black"
      >
        {busy ? 'Minting…' : 'Mint code'}
      </button>
    </form>
  )
}

// ---------- one-time reveal banners ----------

function OneTimeTokenBanner({
  created,
  onDismiss,
}: {
  created: CreatedAgent
  onDismiss: () => void
}) {
  return (
    <div className="rounded border border-amber-500/50 bg-amber-500/10 p-4">
      <div className="mb-1 text-sm font-semibold text-amber-900 dark:text-amber-200">
        Copy this token now. It cannot be retrieved after you leave this page.
      </div>
      <div className="mb-2 text-xs opacity-70">
        Identity: <b>{created.name}</b>
      </div>
      <div className="flex items-center gap-2">
        <code className="flex-1 overflow-x-auto rounded bg-black/5 px-2 py-1 font-mono text-xs dark:bg-white/10">
          {created.token}
        </code>
        <button
          onClick={() => void navigator.clipboard.writeText(created.token)}
          className="rounded border border-black/15 px-2 py-1 text-xs dark:border-white/15"
        >
          Copy
        </button>
        <button
          onClick={onDismiss}
          className="rounded border border-black/15 px-2 py-1 text-xs dark:border-white/15"
        >
          Done
        </button>
      </div>
    </div>
  )
}

function OneTimeCodeBanner({
  created,
  onDismiss,
}: {
  created: CreatedBootstrapCode
  onDismiss: () => void
}) {
  return (
    <div className="rounded border border-amber-500/50 bg-amber-500/10 p-4">
      <div className="mb-1 text-sm font-semibold text-amber-900 dark:text-amber-200">
        One-time bootstrap code — single-use, save it before leaving.
      </div>
      <div className="mb-2 text-xs opacity-70">
        Expires {new Date(created.expires_at).toLocaleString()}
        {created.note ? ` · ${created.note}` : ''}
      </div>
      <div className="flex items-center gap-2">
        <code className="flex-1 overflow-x-auto rounded bg-black/5 px-2 py-1 font-mono text-xs dark:bg-white/10">
          {created.code}
        </code>
        <button
          onClick={() => void navigator.clipboard.writeText(created.code)}
          className="rounded border border-black/15 px-2 py-1 text-xs dark:border-white/15"
        >
          Copy
        </button>
        <button
          onClick={onDismiss}
          className="rounded border border-black/15 px-2 py-1 text-xs dark:border-white/15"
        >
          Done
        </button>
      </div>
    </div>
  )
}
