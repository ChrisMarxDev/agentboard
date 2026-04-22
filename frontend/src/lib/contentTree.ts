// Unified tree over both .md pages and static files. Both live under
// <project>/content/ on disk after CORE_GUIDELINES §9 consolidation; the
// sidebar shows one tree where each leaf is either a renderable page or a
// raw file.
//
// Leaves carry their own URL:
//   page  → `/<path-without-.md>`     (PageRenderer compiles MDX)
//   file  → `/<full-path-with-ext>`   (PageRenderer falls back to FileViewer)

import type { PageEntry } from '../hooks/usePages'
import type { FileEntry } from '../hooks/useFiles'

export interface ContentPageLeaf {
  kind: 'page'
  name: string
  title: string
  path: string
  href: string
  order: number
}

export interface ContentFileLeaf {
  kind: 'file'
  name: string
  title: string
  path: string
  href: string
  entry: FileEntry
}

export interface ContentFolder {
  kind: 'folder'
  name: string
  title: string
  path: string
  href: string // always set — either the indexPage's href or the folder path itself
  indexPage?: ContentPageLeaf
  children: ContentTreeNode[]
}

export type ContentTreeNode = ContentPageLeaf | ContentFileLeaf | ContentFolder

function humanize(seg: string): string {
  const stripped = seg.replace(/^(\d+)[-_.\s]+/, '')
  const spaced = stripped.replace(/[-_]+/g, ' ').trim()
  if (!spaced) return seg
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

interface MutableFolder {
  name: string
  path: string
  folders: Map<string, MutableFolder>
  pages: ContentPageLeaf[]
  files: ContentFileLeaf[]
  indexPage?: ContentPageLeaf
}

function newFolder(name: string, path: string): MutableFolder {
  return { name, path, folders: new Map(), pages: [], files: [] }
}

export function buildContentTree(pages: PageEntry[], files: FileEntry[]): ContentTreeNode[] {
  const root = newFolder('', '')

  // Pages: derive disk path from URL path. `/` → index; else strip leading
  // slash, append .md, the parent segments become folder nodes.
  for (const p of pages) {
    if (p.path === '/') {
      root.indexPage = {
        kind: 'page',
        name: 'index.md',
        title: p.title,
        path: 'index.md',
        href: '/',
        order: p.order,
      }
      continue
    }
    const trimmed = p.path.replace(/^\/+/, '').replace(/\/+$/, '')
    const parts = trimmed.split('/')
    const leaf = parts.pop()!
    let cursor = root
    const acc: string[] = []
    for (const seg of parts) {
      acc.push(seg)
      const key = acc.join('/')
      if (!cursor.folders.has(seg)) {
        cursor.folders.set(seg, newFolder(seg, key))
      }
      cursor = cursor.folders.get(seg)!
    }
    cursor.pages.push({
      kind: 'page',
      name: `${leaf}.md`,
      title: p.title,
      path: parts.concat(leaf).join('/') + '.md',
      href: p.path,
      order: p.order,
    })
  }

  // Files: path is exactly file.name (disk-relative under content/).
  for (const f of files) {
    const parts = f.name.split('/').filter(Boolean)
    const leaf = parts.pop()!
    let cursor = root
    const acc: string[] = []
    for (const seg of parts) {
      acc.push(seg)
      const key = acc.join('/')
      if (!cursor.folders.has(seg)) {
        cursor.folders.set(seg, newFolder(seg, key))
      }
      cursor = cursor.folders.get(seg)!
    }
    cursor.files.push({
      kind: 'file',
      name: leaf,
      title: leaf,
      path: f.name,
      href: `/${f.name}`,
      entry: f,
    })
  }

  // A folder can have a sibling `.md` at its parent level that becomes the
  // folder's index (e.g. content/features.md + content/features/). Detect by
  // checking pages whose URL matches a folder path.
  attachIndexPages(root, pages)

  return materialize(root)
}

function attachIndexPages(folder: MutableFolder, pages: PageEntry[]) {
  const byUrlPath = new Map(pages.map(p => [p.path.replace(/^\/+/, ''), p]))
  const walk = (f: MutableFolder) => {
    const match = byUrlPath.get(f.path)
    if (match) {
      f.indexPage = {
        kind: 'page',
        name: `${f.name}.md`,
        title: match.title,
        path: `${f.path}.md`,
        href: match.path,
        order: match.order,
      }
    }
    for (const child of f.folders.values()) walk(child)
  }
  walk(folder)
}

function materialize(folder: MutableFolder): ContentTreeNode[] {
  // Children are: sub-folders (sorted by name), then leaves (pages + files) by
  // lowercased name. Pages and files interleave alphabetically so a SKILL.md
  // sits next to its examples.md.
  const folderNodes: ContentFolder[] = [...folder.folders.values()]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(f => ({
      kind: 'folder',
      name: f.name,
      title: humanize(f.name),
      path: f.path,
      href: f.indexPage?.href ?? `/${f.path}`,
      indexPage: f.indexPage,
      children: materialize(f).filter(c => {
        // Drop a page leaf that is ALSO used as this folder's indexPage —
        // showing it twice (once as folder label, once inside) is noisy.
        if (c.kind === 'page' && f.indexPage && c.href === f.indexPage.href) return false
        return true
      }),
    }))

  // When a folder at this level has an indexPage, the sibling page leaf with
  // the same href is just the index file on disk — showing both "Skills"
  // (folder) and "Skills" (page) in the same list is redundant. The folder
  // handles the click.
  const siblingIndexHrefs = new Set(
    folderNodes.map(f => f.indexPage?.href).filter((h): h is string => !!h)
  )

  const leaves: (ContentPageLeaf | ContentFileLeaf)[] = [...folder.pages, ...folder.files]
    .filter(leaf => !(leaf.kind === 'page' && siblingIndexHrefs.has(leaf.href)))
    .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))

  return [...folderNodes, ...leaves]
}

export function collectContentFolderPaths(nodes: ContentTreeNode[]): string[] {
  const out: string[] = []
  const walk = (ns: ContentTreeNode[]) => {
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

/**
 * Filter a content tree by a case-insensitive query string. Matches against
 * each node's name, title, and path. A folder is kept if it matches directly
 * OR if any descendant matches — subtrees are pruned to the minimum that still
 * shows each match in its hierarchy context. Returns both the filtered tree
 * and the set of folder paths that should be auto-expanded so every match is
 * visible without manual clicking.
 */
export function filterContentTree(
  nodes: ContentTreeNode[],
  query: string
): { nodes: ContentTreeNode[]; expandedPaths: Set<string> } {
  const q = query.trim().toLowerCase()
  if (!q) return { nodes, expandedPaths: new Set() }

  const expanded = new Set<string>()

  const nodeMatches = (n: ContentTreeNode): boolean => {
    const haystacks =
      n.kind === 'folder'
        ? [n.name, n.title, n.path]
        : n.kind === 'page'
          ? [n.name, n.title, n.path]
          : [n.name, n.path]
    return haystacks.some(h => h.toLowerCase().includes(q))
  }

  const walk = (ns: ContentTreeNode[]): ContentTreeNode[] => {
    const out: ContentTreeNode[] = []
    for (const n of ns) {
      if (n.kind !== 'folder') {
        if (nodeMatches(n)) out.push(n)
        continue
      }
      const self = nodeMatches(n)
      const filteredChildren = walk(n.children)
      if (self || filteredChildren.length > 0) {
        expanded.add(n.path)
        out.push({ ...n, children: filteredChildren })
      }
    }
    return out
  }

  return { nodes: walk(nodes), expandedPaths: expanded }
}

/**
 * Find a folder node in the tree by its path (disk-relative, no leading slash).
 * Returns null if no folder matches.
 */
export function findFolder(nodes: ContentTreeNode[], path: string): ContentFolder | null {
  for (const n of nodes) {
    if (n.kind !== 'folder') continue
    if (n.path === path) return n
    if (path.startsWith(n.path + '/')) {
      const nested = findFolder(n.children, path)
      if (nested) return nested
    }
  }
  return null
}

/**
 * Flatten the content tree to page hrefs in the same DFS order the sidebar
 * renders: at each level, folder indexPages come before descending into the
 * folder's children, and folders come before sibling page leaves. File leaves
 * are skipped (j/k navigation targets pages). Used to keep keyboard navigation
 * in lockstep with the visible sidebar ordering.
 */
export function flattenContentTreePageHrefs(nodes: ContentTreeNode[]): string[] {
  const out: string[] = []
  const walk = (ns: ContentTreeNode[]) => {
    for (const n of ns) {
      if (n.kind === 'folder') {
        if (n.indexPage) out.push(n.indexPage.href)
        walk(n.children)
      } else if (n.kind === 'page') {
        out.push(n.href)
      }
    }
  }
  walk(nodes)
  return out
}

export function ancestorFolderPathsForHref(href: string): string[] {
  const parts = href.replace(/^\/+/, '').replace(/\/+$/, '').split('/').filter(Boolean)
  if (parts.length < 2) return []
  const out: string[] = []
  for (let i = 1; i < parts.length; i++) out.push(parts.slice(0, i).join('/'))
  return out
}
