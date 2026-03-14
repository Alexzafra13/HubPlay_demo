import { useEffect, useState } from 'react'

/**
 * Debounces a value by the specified delay.
 *
 * The returned value only updates after the caller stops changing the input
 * value for at least `delay` milliseconds.
 */
export function useDebounce<T>(value: T, delay = 300): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value)

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedValue(value)
    }, delay)

    return () => {
      clearTimeout(timer)
    }
  }, [value, delay])

  return debouncedValue
}
