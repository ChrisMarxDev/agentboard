import { useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { DataProvider } from './hooks/DataContext'
import Layout from './components/shell/Layout'
import PageRenderer from './components/shell/PageRenderer'
import Admin from './routes/Admin'
import Login from './routes/Login'
import { apiFetch, getToken, redirectToLogin } from './lib/session'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Login is outside the normal shell — no Layout, no DataProvider.
            It must render even when we have no valid token. */}
        <Route path="/login" element={<Login />} />
        <Route
          path="*"
          element={
            <SessionGate>
              <DataProvider>
                <Layout>
                  <Routes>
                    <Route path="/admin" element={<Admin />} />
                    <Route path="*" element={<PageRenderer />} />
                  </Routes>
                </Layout>
              </DataProvider>
            </SessionGate>
          }
        />
      </Routes>
    </BrowserRouter>
  )
}

// SessionGate checks at mount whether the server requires auth. When it
// does and no token is stored, it redirects to /login. When it doesn't
// (fresh install, open-mode loopback), children render immediately.
//
// The check is GET /api/health + a token-required probe: /api/health is
// always open, so hitting it tells us the server is alive; /api/data
// without a token tells us whether auth is on. We use the token-less
// probe on /api/data because it's the smallest gated endpoint.
function SessionGate({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false)

  useEffect(() => {
    if (getToken()) {
      setReady(true)
      return
    }
    // No token — check whether the server needs one.
    let cancelled = false
    ;(async () => {
      try {
        const res = await apiFetch('/api/data', { skipAuth: true })
        if (cancelled) return
        if (res.status === 401) {
          redirectToLogin('missing')
          return
        }
        setReady(true)
      } catch {
        // Network hiccup; let the shell render. Individual fetches will
        // redirect on 401 via apiFetch.
        if (!cancelled) setReady(true)
      }
    })()
    return () => { cancelled = true }
  }, [])

  if (!ready) return null
  return <>{children}</>
}
