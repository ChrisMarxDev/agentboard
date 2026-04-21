import { useEffect, useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { DataProvider } from './hooks/DataContext'
import Layout from './components/shell/Layout'
import PageRenderer from './components/shell/PageRenderer'
import Setup from './routes/Setup'
import Login from './routes/Login'
import Admin from './routes/Admin'
import { fetchMe, type Me } from './lib/auth'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Auth-realm routes stand outside the regular Layout / DataProvider so
            they don't require an agent token or a populated project to render. */}
        <Route path="/setup" element={<Setup />} />
        <Route path="/login" element={<Login />} />
        <Route path="/admin" element={<AdminGate />} />
        {/* Everything else is the data/content SPA. */}
        <Route
          path="*"
          element={
            <DataProvider>
              <Layout>
                <Routes>
                  <Route path="*" element={<PageRenderer />} />
                </Routes>
              </Layout>
            </DataProvider>
          }
        />
      </Routes>
    </BrowserRouter>
  )
}

// AdminGate checks for a live session via /api/admin/me. Unauthenticated
// users are bounced to /login; authenticated admins see the admin UI.
function AdminGate() {
  const [state, setState] = useState<
    { status: 'loading' } | { status: 'unauth' } | { status: 'ok'; me: Me }
  >({ status: 'loading' })

  useEffect(() => {
    let cancelled = false
    fetchMe()
      .then((me) => {
        if (cancelled) return
        setState(me ? { status: 'ok', me } : { status: 'unauth' })
      })
      .catch(() => {
        if (!cancelled) setState({ status: 'unauth' })
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (state.status === 'loading') {
    return <div className="p-6 text-sm opacity-60">Checking session…</div>
  }
  if (state.status === 'unauth') {
    return <Navigate to="/login" replace />
  }
  return <Admin adminName={state.me.name} />
}
