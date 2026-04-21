import { describe, it, expect } from 'vitest'
import {
  ancestorFolderPaths,
  buildPageTree,
  collectFolderPaths,
  humanizeSegment,
  type FolderNode,
  type PageNode,
} from './pageTree'
import type { PageEntry } from '../hooks/usePages'

const p = (path: string, title: string, order: number): PageEntry => ({ path, title, order })

describe('humanizeSegment', () => {
  it('title-cases kebab and snake', () => {
    expect(humanizeSegment('auth-tokens')).toBe('Auth tokens')
    expect(humanizeSegment('hello_world')).toBe('Hello world')
  })
  it('strips leading numeric prefix', () => {
    expect(humanizeSegment('01-coffee')).toBe('Coffee')
    expect(humanizeSegment('10_intro')).toBe('Intro')
  })
  it('falls back to the original when stripping empties it', () => {
    expect(humanizeSegment('01')).toBe('01')
  })
})

describe('ancestorFolderPaths', () => {
  it('returns nothing for root or single-segment paths', () => {
    expect(ancestorFolderPaths('/')).toEqual([])
    expect(ancestorFolderPaths('/home')).toEqual([])
  })
  it('returns each ancestor folder for a nested page', () => {
    expect(ancestorFolderPaths('/features/auth/tokens')).toEqual(['features', 'features/auth'])
  })
})

describe('buildPageTree', () => {
  it('returns a flat list when no pages have folders', () => {
    const tree = buildPageTree([p('/a', 'A', 2), p('/b', 'B', 1)])
    expect(tree.map(n => (n as PageNode).page.path)).toEqual(['/b', '/a'])
  })

  it('pins the index page to the very front', () => {
    const tree = buildPageTree([p('/a', 'A', 5), p('/', 'Home', 0), p('/b', 'B', 1)])
    expect((tree[0] as PageNode).page.path).toBe('/')
  })

  it('groups single-level folders and sorts pages within by order', () => {
    const tree = buildPageTree([
      p('/features/auth', 'Auth', 3),
      p('/features/data-store', 'Data store', 1),
      p('/architecture', 'Architecture', 2),
    ])
    expect(tree).toHaveLength(2)
    expect((tree[0] as PageNode).page.path).toBe('/architecture')
    const folder = tree[1] as FolderNode
    expect(folder.kind).toBe('folder')
    expect(folder.name).toBe('Features')
    expect(folder.indexPage).toBeUndefined()
    expect(folder.children.map(c => (c as PageNode).page.path)).toEqual([
      '/features/data-store',
      '/features/auth',
    ])
  })

  it('handles multi-level nesting with unified order-based sibling sort', () => {
    const tree = buildPageTree([
      p('/features/auth/tokens', 'Tokens', 1),
      p('/features/auth/sessions', 'Sessions', 2),
      p('/features/data-store', 'Data store', 3),
    ])
    const features = tree[0] as FolderNode
    expect(features.name).toBe('Features')
    expect(features.children).toHaveLength(2)
    expect(features.children[0].kind).toBe('page')
    expect((features.children[0] as PageNode).page.path).toBe('/features/data-store')
    const auth = features.children[1] as FolderNode
    expect(auth.kind).toBe('folder')
    expect(auth.name).toBe('Auth')
    expect(auth.indexPage).toBeUndefined()
    expect(auth.children.map(c => (c as PageNode).page.path)).toEqual([
      '/features/auth/tokens',
      '/features/auth/sessions',
    ])
  })

  it('attaches a same-named page as the folder index (merges sibling rows)', () => {
    const tree = buildPageTree([
      p('/features', 'Features', 2),
      p('/features/auth', 'Auth', 5),
      p('/architecture', 'Architecture', 1),
    ])
    expect(tree).toHaveLength(2)
    expect((tree[0] as PageNode).page.path).toBe('/architecture')
    const folder = tree[1] as FolderNode
    expect(folder.kind).toBe('folder')
    expect(folder.name).toBe('Features')
    expect(folder.indexPage?.path).toBe('/features')
    expect(folder.children.map(c => (c as PageNode).page.path)).toEqual(['/features/auth'])
  })

  it('uses indexPage order to merge folder into sibling sort', () => {
    const tree = buildPageTree([
      p('/a', 'A', 1),
      p('/c', 'C', 3),
      p('/b', 'B', 2),
      p('/b/child', 'Child', 99),
    ])
    expect(tree.map(n =>
      n.kind === 'page' ? n.page.path : `folder:${n.path}`,
    )).toEqual(['/a', 'folder:b', '/c'])
    const bFolder = tree[1] as FolderNode
    expect(bFolder.indexPage?.path).toBe('/b')
  })

  it('sorts index-less folders after pages by name', () => {
    const tree = buildPageTree([
      p('/zebra', 'Zebra', 10),
      p('/alpha/child', 'Child', 1),
      p('/apple', 'Apple', 5),
    ])
    expect(tree.map(n =>
      n.kind === 'page' ? n.page.path : `folder:${n.path}`,
    )).toEqual(['/apple', '/zebra', 'folder:alpha'])
  })

  it('preserves humanization of folder display names', () => {
    const tree = buildPageTree([p('/01-prompts/coffee', 'Coffee', 1)])
    expect((tree[0] as FolderNode).name).toBe('Prompts')
  })
})

describe('collectFolderPaths', () => {
  it('returns every folder path in the tree including nested ones', () => {
    const tree = buildPageTree([
      p('/features/auth/tokens', 'T', 1),
      p('/prompts/howto', 'H', 2),
    ])
    expect(collectFolderPaths(tree).sort()).toEqual(
      ['features', 'features/auth', 'prompts'].sort(),
    )
  })
})
