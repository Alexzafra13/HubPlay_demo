// Per-user preferences (key/value store backed by /me/preferences).
//
// Components that persist a UI choice across sessions AND devices use
// `useUserPreference(key, default)` instead of localStorage so the
// user's laptop and phone stay in sync. Values are stored as strings
// server-side; the typed wrapper handles JSON encoding/decoding.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";

export function useMyPreferences() {
  return useQuery<Record<string, string>>({
    queryKey: queryKeys.myPreferences,
    queryFn: () => api.getMyPreferences(),
    staleTime: 30_000,
  });
}

export function useSetMyPreference() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { key: string; value: string }>({
    mutationFn: ({ key, value }) => api.setMyPreference(key, value),
    // Optimistic update: write the new value into the cache immediately
    // so the UI doesn't flash the old value while the request flies.
    // Roll back to the prior map on failure.
    onMutate: async ({ key, value }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.myPreferences });
      const previous = queryClient.getQueryData<Record<string, string>>(
        queryKeys.myPreferences,
      );
      queryClient.setQueryData<Record<string, string>>(
        queryKeys.myPreferences,
        (old) => ({ ...(old ?? {}), [key]: value }),
      );
      return { previous };
    },
    onError: (_err, _vars, ctx) => {
      const prev = (ctx as { previous?: Record<string, string> } | undefined)?.previous;
      if (prev) {
        queryClient.setQueryData(queryKeys.myPreferences, prev);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.myPreferences });
    },
  });
}

/**
 * useUserPreference — typed wrapper over the key/value store. JSON-encodes
 * the value when setting and JSON-decodes when reading. Defaults to
 * `fallback` while the preferences query is still loading or when the key
 * is unset; never returns undefined so callers can render unconditionally.
 */
export function useUserPreference<T>(key: string, fallback: T) {
  const { data } = useMyPreferences();
  const setter = useSetMyPreference();

  let value: T = fallback;
  const raw = data?.[key];
  if (raw !== undefined) {
    try {
      value = JSON.parse(raw) as T;
    } catch {
      // Corrupt value (hand-edited DB, bad migration, etc.): fall back
      // silently rather than crash the surface that depends on it.
      value = fallback;
    }
  }

  const setValue = (next: T) => {
    setter.mutate({ key, value: JSON.stringify(next) });
  };

  return [value, setValue] as const;
}
