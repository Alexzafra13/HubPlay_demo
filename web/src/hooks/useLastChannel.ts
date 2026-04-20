import { useCallback } from "react";
import { useLocalStorage } from "./useLocalStorage";

const STORAGE_KEY = "livetv:lastChannel";

/**
 * Persists the id of the most recently watched channel so the landing view
 * can surface a "Continue watching" entry on the next visit. Returning
 * `null` means the user hasn't watched anything yet (or cleared it).
 */
export function useLastChannel() {
  const [lastChannelId, setLastChannelId] = useLocalStorage<string | null>(
    STORAGE_KEY,
    null,
  );

  const setLastChannel = useCallback(
    (channelId: string | null) => {
      setLastChannelId(channelId);
    },
    [setLastChannelId],
  );

  return { lastChannelId, setLastChannel };
}
