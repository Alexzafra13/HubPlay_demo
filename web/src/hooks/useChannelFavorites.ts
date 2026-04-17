import { useCallback, useMemo } from "react";
import { useLocalStorage } from "./useLocalStorage";

const STORAGE_KEY = "livetv:favorites";

/**
 * Favourite channels tracked by id. Persisted in localStorage so they
 * survive page reloads and sync across tabs via the storage event.
 *
 * The underlying value is stored as an array for JSON-serialisation
 * safety; the hook exposes a Set for O(1) membership checks plus a
 * typed toggler.
 */
export function useChannelFavorites() {
  const [raw, setRaw] = useLocalStorage<string[]>(STORAGE_KEY, []);

  const favorites = useMemo(() => new Set(raw), [raw]);

  const toggleFavorite = useCallback(
    (channelId: string) => {
      setRaw((prev) => {
        const next = new Set(prev);
        if (next.has(channelId)) next.delete(channelId);
        else next.add(channelId);
        return Array.from(next);
      });
    },
    [setRaw],
  );

  const isFavorite = useCallback(
    (channelId: string) => favorites.has(channelId),
    [favorites],
  );

  return { favorites, toggleFavorite, isFavorite };
}
