import { useEffect, useState } from 'react'
import { useData } from '../../hooks/useData'
import { resolveFileUrl, type FileRef } from '../../lib/fileUrl'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'

interface ImageProps {
  source?: string
  src?: string
  alt?: string
  width?: number | string
  height?: number | string
  fit?: 'contain' | 'cover' | 'fill' | 'none'
}

export function Image({ source, src, alt, width, height, fit = 'contain' }: ImageProps) {
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

  if (source && loading) return null
  if (!resolved) return null

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
  const finalSrc = nonce > 0 ? appendQuery(resolved, `_=${nonce}`) : resolved

  return (
    <img
      src={finalSrc}
      alt={finalAlt}
      width={finalWidth}
      height={finalHeight}
      loading="lazy"
      className="my-2 rounded-md"
      style={{
        maxWidth: '100%',
        height: finalHeight ? undefined : 'auto',
        objectFit: fit,
        display: 'block',
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
