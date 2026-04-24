// publicMatcher — client-side glob matcher mirroring
// internal/publicroutes/matcher.go. Used by SessionGate to decide
// whether an anonymous visitor should render the app in public mode or
// be bounced to /login.
//
// Glob semantics (same as server):
//   /foo         exact
//   /foo/*       direct children only (one segment)
//   /foo/**      zero or more descendants
//   !/foo/bar    exclusion (applied after includes)

interface Compiled {
  raw: string
  parts: string[]
  negate: boolean
}

function splitPath(p: string): string[] {
  let out = p.split('/')
  if (out.length > 0 && out[0] === '') out = out.slice(1)
  if (out.length > 0 && out[out.length - 1] === '') out = out.slice(0, -1)
  return out
}

function normalisePath(p: string): string {
  if (!p || p === '/') return '/'
  return p.endsWith('/') ? p.slice(0, -1) : p
}

function compile(patterns: string[]): Compiled[] {
  return patterns
    .map(p => p.trim())
    .filter(Boolean)
    .map(p => {
      const negate = p.startsWith('!')
      const raw = negate ? p.slice(1) : p
      const prefixed = raw.startsWith('/') ? raw : `/${raw}`
      return { raw: prefixed, parts: splitPath(prefixed), negate }
    })
}

function matchParts(pat: string[], path: string[]): boolean {
  let pi = 0
  let si = 0
  let starPi = -1
  let starSi = 0
  while (si < path.length) {
    if (pi < pat.length) {
      if (pat[pi] === '**') {
        starPi = pi
        starSi = si
        pi++
        continue
      }
      if (pat[pi] === '*') {
        pi++
        si++
        continue
      }
      if (pat[pi] === path[si]) {
        pi++
        si++
        continue
      }
    }
    if (starPi >= 0) {
      starSi++
      pi = starPi + 1
      si = starSi
      continue
    }
    return false
  }
  while (pi < pat.length && pat[pi] === '**') pi++
  return pi === pat.length
}

export function matchPublic(patterns: string[], path: string): boolean {
  if (!patterns || patterns.length === 0) return false
  const compiled = compile(patterns)
  const includes = compiled.filter(c => !c.negate)
  const excludes = compiled.filter(c => c.negate)
  const parts = splitPath(normalisePath(path))
  const matched = includes.some(inc => matchParts(inc.parts, parts))
  if (!matched) return false
  return !excludes.some(exc => matchParts(exc.parts, parts))
}
