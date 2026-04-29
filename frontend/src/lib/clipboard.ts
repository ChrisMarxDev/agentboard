// copyToClipboard handles the "copy text" action across the app.
//
// The modern Clipboard API (`navigator.clipboard.writeText`) only
// works in secure contexts — HTTPS or localhost. The hosted dogfood
// instance lives on plain HTTP, where every clipboard call rejects
// silently and the user sees nothing happen. To keep "Copy" buttons
// honest in every deployment topology we ship, we try the modern API
// first and fall back to the legacy `document.execCommand('copy')`
// path, which is deprecated but still ships in every browser and
// works on plain HTTP.
//
// Returns `true` when the copy actually landed in the clipboard,
// `false` otherwise — callers can decide whether to surface an
// error or stay silent.
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Permissions blocked or some other rejection — fall through
      // to the legacy path rather than failing loudly.
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined') return false
  const ta = document.createElement('textarea')
  ta.value = text
  // Off-screen but still focusable. Setting display:none would make
  // execCommand('copy') a no-op on most engines.
  ta.style.position = 'fixed'
  ta.style.top = '-9999px'
  ta.style.left = '-9999px'
  ta.style.opacity = '0'
  ta.setAttribute('readonly', '')
  document.body.appendChild(ta)
  const prevSelection = document.getSelection()?.rangeCount
    ? document.getSelection()!.getRangeAt(0)
    : null
  ta.focus()
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  document.body.removeChild(ta)
  // Restore the user's prior text selection if they had one.
  if (prevSelection) {
    const sel = document.getSelection()
    sel?.removeAllRanges()
    sel?.addRange(prevSelection)
  }
  return ok
}
