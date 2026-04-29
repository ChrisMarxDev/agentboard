import { useEffect, useState } from 'react'
import { useData } from '../../hooks/useData'
import { resolveFileUrl, type FileRef } from '../../lib/fileUrl'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'
import { apiFetch } from '../../lib/session'

type Radius = 'none' | 'sm' | 'md' | 'lg' | 'xl' | 'full' | number
type Align = 'left' | 'center' | 'right'

interface ImageProps {
  source?: string
  src?: string
  alt?: string
  width?: number | string
  height?: number | string
  fit?: 'contain' | 'cover' | 'fill' | 'none'
  radius?: Radius
  align?: Align
}

export function Image({ source, src, alt, width, height, fit = 'contain', radius = 'md', align = 'left' }: ImageProps) {
  const { data, loading } = useData(source ?? '')

  // When src is explicit, skip the data hook's round-trip entirely.
  const resolved = src ? src : resolveFileUrl(data as unknown)

  // Cache-bust on SSE file-updated events for matching names so the browser
  // refetches. ETag handling upstream will turn the refetch into a cheap 304
  // when the file didn't actually change.
  const [nonce, setNonce] = useState(0)
  useEffect(() => {
    if (!source) return
    const handler = (e: Event) => {
      const ev = e as CustomEvent<{ name?: string }>
      // Match either the data value's file name or the full resolved URL.
      const ref = data as FileRef | string | undefined
      const fileName = typeof ref === 'string' ? ref : ref?.file
      if (!ev.detail?.name) return
      if (fileName && (fileName === ev.detail.name || fileName.endsWith('/' + ev.detail.name))) {
        setNonce(n => n + 1)
      }
    }
    window.addEventListener('agentboard:file-updated', handler)
    return () => window.removeEventListener('agentboard:file-updated', handler)
  }, [source, data])

  // Same-origin file URLs go through apiFetch → blob URL because <img src=>
  // can't attach a Bearer token. External URLs (https://…, data:…) pass
  // through to the native <img src=> path unchanged.
  const requiresAuth = !!resolved && resolved.startsWith('/')
  const fetchSrc = resolved && nonce > 0 ? appendQuery(resolved, `_=${nonce}`) : resolved
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  useEffect(() => {
    if (!requiresAuth || !fetchSrc) {
      setBlobUrl(null)
      return
    }
    let cancelled = false
    let createdUrl: string | null = null
    apiFetch(fetchSrc)
      .then(res => {
        if (!res.ok) throw new Error(`fetch ${fetchSrc} → ${res.status}`)
        return res.blob()
      })
      .then(blob => {
        if (cancelled) return
        createdUrl = URL.createObjectURL(blob)
        setBlobUrl(createdUrl)
      })
      .catch(err => {
        if (cancelled) return
        beaconError({
          component: 'Image',
          source: source ?? fetchSrc ?? '',
          error: err instanceof Error ? err.message : 'fetch failed',
        })
      })
    return () => {
      cancelled = true
      if (createdUrl) URL.revokeObjectURL(createdUrl)
    }
  }, [requiresAuth, fetchSrc, source])

  if (source && loading) return null
  if (!resolved) return null
  if (requiresAuth && !blobUrl) return null

  // If the data shape carried alt/width/height, let component props still win.
  let dataAlt: string | undefined
  let dataWidth: number | string | undefined
  let dataHeight: number | string | undefined
  if (!src && data && typeof data === 'object') {
    const obj = data as FileRef
    dataAlt = typeof obj.alt === 'string' ? obj.alt : undefined
    dataWidth = typeof obj.width === 'number' ? obj.width : undefined
    dataHeight = typeof obj.height === 'number' ? obj.height : undefined
  }

  const finalAlt = alt ?? dataAlt ?? ''
  const finalWidth = width ?? dataWidth
  const finalHeight = height ?? dataHeight
  const finalSrc = requiresAuth ? blobUrl! : resolved
  const borderRadius = radiusToCss(radius)
  const [marginLeft, marginRight] =
    align === 'center' ? ['auto', 'auto'] :
    align === 'right'  ? ['auto', '0'] :
                         ['0', 'auto']

  return (
    <img
      src={finalSrc}
      alt={finalAlt}
      width={finalWidth}
      height={finalHeight}
      loading="lazy"
      className="my-2"
      style={{
        maxWidth: '100%',
        height: finalHeight ? undefined : 'auto',
        objectFit: fit,
        display: 'block',
        borderRadius,
        marginLeft,
        marginRight,
      }}
      onLoad={() => { resetBeacon('Image', source ?? finalSrc) }}
      onError={() => {
        beaconError({
          component: 'Image',
          source: source ?? finalSrc,
          error: `Failed to load image at ${finalSrc}`,
        })
      }}
    />
  )
}

function appendQuery(url: string, q: string): string {
  return url.includes('?') ? `${url}&${q}` : `${url}?${q}`
}

function radiusToCss(r: Radius): string {
  if (typeof r === 'number') return `${r}px`
  switch (r) {
    case 'none': return '0'
    case 'sm':   return '4px'
    case 'md':   return '6px'
    case 'lg':   return '8px'
    case 'xl':   return '12px'
    case 'full': return '9999px'
  }
}
