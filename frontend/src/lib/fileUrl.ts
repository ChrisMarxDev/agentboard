/**
 * Resolve a string into a URL usable by <img> or <a>. Shared by the Image and
 * File components so their `source`/`src` semantics stay identical.
 *
 *  - `http://…`, `https://…`, `data:…`  → pass through unchanged
 *  - starts with `/`                    → absolute path, unchanged
 *  - anything else                      → `/api/files/<encoded segments>`
 *
 * The input may also be an object shape: `{ file, label?, alt?, ... }`. In
 * that case we resolve the `file` field.
 */
export interface FileRef {
  file?: string
  label?: string
  alt?: string
  width?: number
  height?: number
  [key: string]: unknown
}

export function resolveFileUrl(input: unknown): string | null {
  if (input == null) return null
  let name: string

  if (typeof input === 'string') {
    name = input
  } else if (typeof input === 'object') {
    const obj = input as FileRef
    if (typeof obj.file !== 'string') return null
    name = obj.file
  } else {
    return null
  }

  if (!name) return null
  if (/^(https?:|data:)/i.test(name)) return name
  if (name.startsWith('/')) return name

  // Encode each `/`-separated segment so spaces / unicode survive.
  return '/api/files/' + name.split('/').map(encodeURIComponent).join('/')
}

/** Extract a display name from a ref for File labels (preserves `label` if set). */
export function resolveFileLabel(input: unknown, fallback?: string): string {
  if (input && typeof input === 'object') {
    const obj = input as FileRef
    if (typeof obj.label === 'string' && obj.label) return obj.label
    if (typeof obj.file === 'string') return obj.file.split('/').pop() ?? obj.file
  }
  if (typeof input === 'string') return input.split('/').pop() ?? input
  return fallback ?? ''
}
