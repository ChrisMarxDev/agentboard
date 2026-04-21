import type { PageEntry } from '../hooks/usePages'

export interface PageNode {
  kind: 'page'
  page: PageEntry
}

export interface FolderNode {
  kind: 'folder'
  name: string
  path: string
  indexPage?: PageEntry
  children: TreeNode[]
}

export type TreeNode = PageNode | FolderNode

interface MutableFolder {
  kind: 'folder'
  name: string
  path: string
  childrenByName: Map<string, MutableFolder>
  pages: PageEntry[]
  indexPage?: PageEntry
}

function newFolder(name: string, path: string): MutableFolder {
  return { kind: 'folder', name: humanizeSegment(name), path, childrenByName: new Map(), pages: [] }
}

export function humanizeSegment(seg: string): string {
  const stripped = seg.replace(/^(\d+)[-_.\s]+/, '')
  const spaced = stripped.replace(/[-_]+/g, ' ').trim()
  if (!spaced) return seg
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

export function ancestorFolderPaths(pagePath: string): string[] {
  const trimmed = pagePath.replace(/^\/+/, '').replace(/\/+$/, '')
  if (!trimmed) return []
  const parts = trimmed.split('/')
  if (parts.length < 2) return []
  const out: string[] = []
  for (let i = 1; i < parts.length; i++) {
    out.push(parts.slice(0, i).join('/'))
  }
  return out
}

export function collectFolderPaths(nodes: TreeNode[]): string[] {
  const out: string[] = []
  const walk = (ns: TreeNode[]) => {
    for (const n of ns) {
      if (n.kind === 'folder') {
        out.push(n.path)
        walk(n.children)
      }
    }
  }
  walk(nodes)
  return out
}

export function buildPageTree(pages: PageEntry[]): TreeNode[] {
  const folderPaths = new Set<string>()
  for (const page of pages) {
    const trimmed = page.path.replace(/^\/+/, '')
    if (!trimmed) continue
    const segments = trimmed.split('/')
    for (let i = 1; i < segments.length; i++) {
      folderPaths.add(segments.slice(0, i).join('/'))
    }
  }

  const rootFolders = new Map<string, MutableFolder>()
  const rootPages: PageEntry[] = []
  let indexPage: PageEntry | null = null
  const pageByFolderPath = new Map<string, PageEntry>()

  for (const page of pages) {
    if (page.path === '/') {
      indexPage = page
      continue
    }
    const trimmed = page.path.replace(/^\/+/, '')
    if (folderPaths.has(trimmed)) {
      pageByFolderPath.set(trimmed, page)
      continue
    }
    const segments = trimmed.split('/')
    if (segments.length === 1) {
      rootPages.push(page)
      continue
    }

    const folderSegments = segments.slice(0, -1)
    let parentMap = rootFolders
    let folder: MutableFolder | undefined
    let pathSoFar = ''
    for (const seg of folderSegments) {
      pathSoFar = pathSoFar ? `${pathSoFar}/${seg}` : seg
      let next = parentMap.get(seg)
      if (!next) {
        next = newFolder(seg, pathSoFar)
        parentMap.set(seg, next)
      }
      folder = next
      parentMap = next.childrenByName
    }
    folder!.pages.push(page)
  }

  const attachIndex = (folder: MutableFolder) => {
    const idx = pageByFolderPath.get(folder.path)
    if (idx) folder.indexPage = idx
    for (const child of folder.childrenByName.values()) attachIndex(child)
  }
  for (const f of rootFolders.values()) attachIndex(f)

  const result: TreeNode[] = []
  if (indexPage) result.push({ kind: 'page', page: indexPage })
  for (const item of sortSiblings(rootPages, rootFolders)) result.push(item)
  return result
}

type SiblingItem =
  | { kind: 'page'; page: PageEntry; sortKey: number; name: string }
  | { kind: 'folder'; folder: MutableFolder; sortKey: number; name: string }

function sortSiblings(pages: PageEntry[], folders: Map<string, MutableFolder>): TreeNode[] {
  const items: SiblingItem[] = [
    ...pages.map(p => ({ kind: 'page' as const, page: p, sortKey: p.order, name: p.title })),
    ...Array.from(folders.values()).map(f => ({
      kind: 'folder' as const,
      folder: f,
      sortKey: f.indexPage ? f.indexPage.order : Number.POSITIVE_INFINITY,
      name: f.name,
    })),
  ]
  items.sort((a, b) => {
    if (a.sortKey !== b.sortKey) return a.sortKey - b.sortKey
    return a.name.localeCompare(b.name)
  })
  return items.map(it =>
    it.kind === 'page' ? { kind: 'page', page: it.page } : materialize(it.folder),
  )
}

function materialize(folder: MutableFolder): FolderNode {
  const children = sortSiblings(folder.pages, folder.childrenByName)
  return {
    kind: 'folder',
    name: folder.name,
    path: folder.path,
    indexPage: folder.indexPage,
    children,
  }
}
