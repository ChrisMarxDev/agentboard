import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { login } from '../lib/auth'

export default function Login() {
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await login(name.trim(), password)
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
        <h1 className="text-2xl font-semibold">Admin login</h1>
        <p className="mt-2 text-sm opacity-70">
          Only admins of this AgentBoard instance can log in. Agents use
          tokens on data endpoints directly — they don't need to log in.
        </p>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Admin name</span>
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="rounded border border-black/15 bg-white px-3 py-2 dark:border-white/15 dark:bg-black"
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="opacity-70">Password</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="rounded border border-black/15 bg-white px-3 py-2 dark:border-white/15 dark:bg-black"
            required
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
          {busy ? 'Logging in…' : 'Log in'}
        </button>
      </form>

      <p className="text-xs opacity-60">
        Forgot your password? Run{' '}
        <code className="rounded bg-black/5 px-1 dark:bg-white/5">
          agentboard admin reset
        </code>{' '}
        on the host to mint a fresh bootstrap code, then visit /setup.
      </p>
    </div>
  )
}
