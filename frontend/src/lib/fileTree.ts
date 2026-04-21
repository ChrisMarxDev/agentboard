export interface FileEntry {
  name: string
  size: number
  content_type: string
  modified_at: string
}

export interface FileNode {
  kind: 'file'
  name: string
  path: string
  entry: FileEntry
}

export interface FileFolderNode {
  kind: 'folder'
  name: string
  path: string
  children: FileTreeNode[]
}

export type FileTreeNode = FileNode | FileFolderNode

interface MutableFolder {
  name: string
  path: string
  folders: Map<string, MutableFolder>
  files: FileEntry[]
}

function newFolder(name: string, path: string): MutableFolder {
  return { name, path, folders: new Map(), files: [] }
}

export function buildFileTree(entries: FileEntry[]): FileTreeNode[] {
  const root = newFolder('', '')
  for (const e of entries) {
    const parts = e.name.split('/').filter(Boolean)
    const leaf = parts.pop()!
    let cursor = root
    const pathSoFar: string[] = []
    for (const seg of parts) {
      pathSoFar.push(seg)
      const key = pathSoFar.join('/')
      if (!cursor.folders.has(seg)) {
        cursor.folders.set(seg, newFolder(seg, key))
      }
      cursor = cursor.folders.get(seg)!
    }
    cursor.files.push({ ...e, name: leaf })
  }
  return materialize(root)
}

function materialize(folder: MutableFolder): FileTreeNode[] {
  const folderNodes: FileFolderNode[] = [...folder.folders.values()]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(f => ({
      kind: 'folder',
      name: f.name,
      path: f.path,
      children: materialize(f),
    }))
  const fileNodes: FileNode[] = folder.files
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(entry => ({
      kind: 'file',
      name: entry.name,
      path: buildPath(folder.path, entry.name),
      entry,
    }))
  return [...folderNodes, ...fileNodes]
}

function buildPath(folderPath: string, leaf: string): string {
  return folderPath ? `${folderPath}/${leaf}` : leaf
}

export function collectFileFolderPaths(nodes: FileTreeNode[]): string[] {
  const out: string[] = []
  const walk = (ns: FileTreeNode[]) => {
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

export function ancestorFolderPathsForFile(filePath: string): string[] {
  const parts = filePath.split('/').filter(Boolean)
  if (parts.length < 2) return []
  const out: string[] = []
  for (let i = 1; i < parts.length; i++) out.push(parts.slice(0, i).join('/'))
  return out
}
