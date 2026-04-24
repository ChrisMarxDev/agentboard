import { useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes, useLocation } from 'react-router-dom'
import { DataProvider } from './hooks/DataContext'
import Layout from './components/shell/Layout'
import PageRenderer from './components/shell/PageRenderer'
import Admin from './routes/Admin'
import Login from './routes/Login'
import { getToken, redirectToLogin, setPublicMode } from './lib/session'
import { matchPublic } from './lib/publicMatcher'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* /login renders outside the shell — must work without a token
            and without the data context. */}
        <Route path="/login" element={<Login />} />
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
// location. When the user navigates between pages the provider is
// re-mounted with a new path, which forces a fresh view/open against
// the broker — no bleed of one page's data into another's scope.
function ViewRouter() {
  const location = useLocation()
  const pagePath =
    location.pathname === '/' ? 'index' : location.pathname.replace(/^\//, '')
  // Admin route has no page behind it — bypass the view broker entirely.
  if (location.pathname === '/admin') {
    return (
      <DataProvider path={null}>
        <Layout>
          <Admin />
        </Layout>
      </DataProvider>
    )
  }
  return (
    <DataProvider key={pagePath} path={pagePath}>
      <Layout>
        <PageRenderer />
      </Layout>
    </DataProvider>
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
