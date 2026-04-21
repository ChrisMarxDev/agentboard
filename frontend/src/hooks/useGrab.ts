import { useEffect, useState } from 'react'
import { getMode, getPicks, subscribe, type Pick } from '../lib/grab'

export interface GrabState {
  mode: boolean
  picks: Pick[]
}

/** Subscribe to the current grab-mode flag + picks list. */
export function useGrab(): GrabState {
  const [state, setState] = useState<GrabState>(() => ({
    mode: getMode(),
    picks: getPicks(),
  }))

  useEffect(() => {
    const refresh = () => setState({ mode: getMode(), picks: getPicks() })
    return subscribe(refresh)
  }, [])

  return state
}
