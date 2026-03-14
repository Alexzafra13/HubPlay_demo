import { useCallback, useEffect, useState } from 'react'

/**
 * Reads and writes a value to `localStorage`, with SSR safety.
 *
 * On the server (or when `localStorage` is unavailable) the hook falls back to
 * the provided `initialValue` without throwing.
 */
export function useLocalStorage<T>(
  key: string,
  initialValue: T,
): [T, (value: T | ((prev: T) => T)) => void] {
  // Lazy initialiser — only runs on the first render.
  const [storedValue, setStoredValue] = useState<T>(() => {
    if (typeof window === 'undefined') return initialValue

    try {
      const item = localStorage.getItem(key)
      return item !== null ? (JSON.parse(item) as T) : initialValue
    } catch {
      return initialValue
    }
  })

  // Persist to localStorage whenever the value changes.
  const setValue = useCallback(
    (value: T | ((prev: T) => T)) => {
      setStoredValue((prev) => {
        const nextValue =
          value instanceof Function ? value(prev) : value

        if (typeof window !== 'undefined') {
          try {
            localStorage.setItem(key, JSON.stringify(nextValue))
          } catch {
            // Storage full or unavailable — silently ignore.
          }
        }

        return nextValue
      })
    },
    [key],
  )

  // Sync across tabs via the `storage` event.
  useEffect(() => {
    if (typeof window === 'undefined') return

    function handleStorageChange(e: StorageEvent) {
      if (e.key !== key) return

      try {
        const newValue =
          e.newValue !== null
            ? (JSON.parse(e.newValue) as T)
            : initialValue
        setStoredValue(newValue)
      } catch {
        setStoredValue(initialValue)
      }
    }

    window.addEventListener('storage', handleStorageChange)
    return () => {
      window.removeEventListener('storage', handleStorageChange)
    }
  }, [key, initialValue])

  return [storedValue, setValue]
}
