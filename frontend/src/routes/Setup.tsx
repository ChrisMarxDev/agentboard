import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { setup } from '../lib/auth'

// Setup is the only page that accepts a bootstrap code. The code is minted
// either by the installer (first boot) or by `agentboard admin bootstrap-code`
// on the host. Submitting this form consumes the code and creates the admin
// identity in one step.

export default function Setup() {
  const navigate = useNavigate()
  const [code, setCode] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (password.length < 8) {
      setError('Password must be at least 8 characters.')
      return
    }
    if (password !== confirm) {
      setError('Passwords do not match.')
      return
    }
    setBusy(true)
    try {
      await setup(code.trim(), name.trim(), password)
      navigate('/admin', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Claim admin access</h1>
        <p className="mt-2 text-sm opacity-70">
          Paste the one-time bootstrap code printed by the installer, or
          run{' '}
          <code className="rounded bg-black/5 px-1 dark:bg-white/5">
            agentboard admin bootstrap-code
          </code>{' '}
          on the host to generate a new one. The code is consumed on submit.
        </p>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Bootstrap code</span>
          <input
            autoFocus
            value={code}
            onChange={(e) => setCode(e.target.value)}
            className="rounded border border-black/15 bg-white px-3 py-2 font-mono dark:border-white/15 dark:bg-black"
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Admin name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="rounded border border-black/15 bg-white px-3 py-2 dark:border-white/15 dark:bg-black"
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Password (min 8 chars)</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="rounded border border-black/15 bg-white px-3 py-2 dark:border-white/15 dark:bg-black"
            required
            minLength={8}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Confirm password</span>
          <input
            type="password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            className="rounded border border-black/15 bg-white px-3 py-2 dark:border-white/15 dark:bg-black"
            required
            minLength={8}
          />
        </label>

        {error && (
          <div className="rounded border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300">
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={busy}
          className="mt-2 rounded bg-black px-4 py-2 font-medium text-white disabled:opacity-60 dark:bg-white dark:text-black"
        >
          {busy ? 'Creating…' : 'Create admin'}
        </button>
      </form>
    </div>
  )
}
