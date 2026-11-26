import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Countdown guard for rate-limited actions (HTTP 429 + Retry-After).
 *
 * The API rejects bursts with a delta-seconds Retry-After header, but an
 * instantly dismissed toast leaves the user with no idea why the button "did
 * nothing". This hook exposes the remaining seconds so the UI can keep the
 * submit button disabled and render a visible countdown until the window
 * resets.
 */
export function useRetryCountdown() {
  const [remaining, setRemaining] = useState(0)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stop = useCallback(() => {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const start = useCallback(
    (seconds: number) => {
      stop()
      // Guard against a missing header so the button never locks forever.
      const total = Math.max(1, Math.ceil(seconds))
      setRemaining(total)
      timerRef.current = setInterval(() => {
        setRemaining((prev) => {
          if (prev <= 1) {
            stop()
            return 0
          }
          return prev - 1
        })
      }, 1000)
    },
    [stop]
  )

  useEffect(() => stop, [stop])

  return { remaining, isCoolingDown: remaining > 0, start }
}
