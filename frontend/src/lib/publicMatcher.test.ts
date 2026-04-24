import { describe, it, expect } from 'vitest'
import { matchPublic } from './publicMatcher'

describe('matchPublic', () => {
  it('returns false for empty pattern list', () => {
    expect(matchPublic([], '/anything')).toBe(false)
  })

  it('matches exact paths', () => {
    expect(matchPublic(['/changelog'], '/changelog')).toBe(true)
    expect(matchPublic(['/changelog'], '/changelog/')).toBe(true)
    expect(matchPublic(['/changelog'], '/other')).toBe(false)
  })

  it('matches root "/" exactly', () => {
    expect(matchPublic(['/'], '/')).toBe(true)
    expect(matchPublic(['/'], '/foo')).toBe(false)
  })

  it('handles single-segment wildcard *', () => {
    expect(matchPublic(['/catalog/*'], '/catalog/a')).toBe(true)
    expect(matchPublic(['/catalog/*'], '/catalog/a/b')).toBe(false)
    expect(matchPublic(['/catalog/*'], '/catalog')).toBe(false)
  })

  it('handles zero-or-more wildcard **', () => {
    expect(matchPublic(['/skills/**'], '/skills')).toBe(true)
    expect(matchPublic(['/skills/**'], '/skills/a')).toBe(true)
    expect(matchPublic(['/skills/**'], '/skills/a/b/c')).toBe(true)
    expect(matchPublic(['/skills/**'], '/other')).toBe(false)
  })

  it('applies exclusions after includes', () => {
    const p = ['/blog/**', '!/blog/drafts/**']
    expect(matchPublic(p, '/blog/post-1')).toBe(true)
    expect(matchPublic(p, '/blog/drafts/wip')).toBe(false)
    expect(matchPublic(p, '/blog/drafts/deep/path')).toBe(false)
  })

  it('tolerates patterns without leading slash', () => {
    expect(matchPublic(['skills/**'], '/skills/foo')).toBe(true)
  })

  it('matches ** in the middle', () => {
    const p = ['/api/**/public']
    expect(matchPublic(p, '/api/a/public')).toBe(true)
    expect(matchPublic(p, '/api/a/b/c/public')).toBe(true)
    expect(matchPublic(p, '/api/public')).toBe(true) // ** matches 0 segments
    expect(matchPublic(p, '/api/a/private')).toBe(false)
  })

  it('exclusion order does not matter', () => {
    const p = ['!/internal/**', '/', '/internal/**']
    expect(matchPublic(p, '/internal/secret')).toBe(false)
    expect(matchPublic(p, '/')).toBe(true)
  })
})
