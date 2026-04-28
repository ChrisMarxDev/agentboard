import { useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes, useLocation } from 'react-router-dom'
import { DataProvider } from './hooks/DataContext'
import Layout from './components/shell/Layout'
import PageRenderer from './components/shell/PageRenderer'
import Admin from './routes/Admin'
import Login from './routes/Login'
import InviteRedeem from './routes/InviteRedeem'
import Tokens from './routes/Tokens'
import InboxPage from './routes/Inbox'
import { getToken, redirectToLogin, setPublicMode } from './lib/session'
import { matchPublic } from './lib/publicMatcher'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* /login + /invite/:id render outside the shell — both must
            work without a token and without the data context. */}
        <Route path="/login" element={<Login />} />
        <Route path="/invite/:id" element={<InviteRedeem />} />
        <Route
          path="*"
          element={
            <SessionGate>
              <ViewRouter />
            </SessionGate>
          }
        />
      </Routes>
    </BrowserRouter>
  )
}

// ViewRouter wraps the app in a DataProvider scoped to the current
// location. The keyed DataProvider sits *inside* Layout so that the
// shell (sidebar, drawer, keyboard shortcuts, theme switch) survives
// navigation while the page bundle re-fetches on every path change —
// the key forces a fresh view/open with no bleed between scopes.
function ViewRouter() {
  const location = useLocation()
  const pagePath =
    location.pathname === '/' ? 'index' : location.pathname.replace(/^\//, '')
  // Admin and Tokens routes have no page behind them — bypass the view
  // broker entirely.
  if (location.pathname === '/admin') {
    return (
      <Layout>
        <DataProvider path={null}>
          <Admin />
        </DataProvider>
      </Layout>
    )
  }
  if (location.pathname === '/tokens') {
    return (
      <Layout>
        <DataProvider path={null}>
          <Tokens />
        </DataProvider>
      </Layout>
    )
  }
  if (location.pathname === '/inbox') {
    return (
      <Layout>
        <DataProvider path={null}>
          <InboxPage />
        </DataProvider>
      </Layout>
    )
  }
  return (
    <Layout>
      <DataProvider key={pagePath} path={pagePath}>
        <PageRenderer />
      </DataProvider>
    </Layout>
  )
}

// SessionGate: three entry flows, in priority order.
//
//   1. URL fragment carries a share token (`#share=sh_...`) → redeem it
//      into a view-session cookie, strip the fragment, render.
//   2. localStorage bearer present → render as that user.
//   3. Current path matches public.paths → render anonymously.
//   4. Else → /login with a `next` hint.
function SessionGate({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false)

  useEffect(() => {
    ;(async () => {
      // 1. Fragment share → redeem to cookie.
      const frag = readFragmentShare()
      if (frag) {
        try {
          const res = await fetch('/api/share/redeem', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ token: frag }),
            credentials: 'include',
          })
          if (res.ok) {
            setPublicMode(true)
            // Strip the fragment so the token doesn't persist in the URL.
            window.history.replaceState(null, '', window.location.pathname + window.location.search)
            setReady(true)
            return
          }
        } catch {
          // fall through to login
        }
      }

      // 2. Bearer in localStorage.
      if (getToken()) {
        setReady(true)
        return
      }

      // 3. Public-routes match.
      try {
        const res = await fetch('/api/config')
        if (res.ok) {
          const cfg = (await res.json()) as { public?: { paths?: string[] } }
          const paths = cfg.public?.paths ?? []
          const path = window.location.pathname
          if (matchPublic(paths, path)) {
            setPublicMode(true)
            setReady(true)
            return
          }
        }
      } catch {
        // fall through
      }

      // 4. Nothing — bounce to login.
      redirectToLogin('missing')
    })()
  }, [])

  if (!ready) return null
  return <>{children}</>
}

function readFragmentShare(): string | null {
  if (typeof window === 'undefined') return null
  const h = window.location.hash || ''
  const m = h.match(/[#&]share=([^&]+)/)
  if (!m) return null
  try {
    return decodeURIComponent(m[1])
  } catch {
    return m[1]
  }
}
