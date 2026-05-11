// Watch-progress + favorites toggling. Each mutation invalidates the
// surfaces that changed: progress hits item detail + the
// continue-watching rail; favorite toggles refresh the favourites
// list; mark-played feeds both continue-watching and next-up.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { UserData } from "../types";

export function useUpdateProgress() {
  const queryClient = useQueryClient();
  return useMutation<
    UserData,
    Error,
    {
      itemId: string;
      data: {
        position_ticks?: number;
        audio_stream_index?: number;
        subtitle_stream_index?: number;
      };
    }
  >({
    mutationFn: ({ itemId, data }) => api.updateProgress(itemId, data),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.progress(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
    },
  });
}

export function useToggleFavorite() {
  const queryClient = useQueryClient();
  return useMutation<UserData, Error, string>({
    mutationFn: (itemId) => api.toggleFavorite(itemId),
    onSuccess: (_data, itemId) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.favorites });
    },
  });
}

export function useMarkPlayed() {
  const queryClient = useQueryClient();
  return useMutation<UserData, Error, string>({
    mutationFn: (itemId) => api.markPlayed(itemId),
    onSuccess: (_data, itemId) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
      queryClient.invalidateQueries({ queryKey: queryKeys.nextUp });
    },
  });
}

// useRemoveFromContinueWatching dismisses a row from the Continue
// Watching rail. Optimistic: the card disappears the instant the user
// clicks. On error we roll back so the rail re-shows the row instead of
// silently swallowing the failure.
export function useRemoveFromContinueWatching() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string, { previous: unknown }>({
    mutationFn: (itemId) => api.removeFromContinueWatching(itemId),
    onMutate: async (itemId) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.continueWatching });
      const previous = queryClient.getQueryData(queryKeys.continueWatching);
      queryClient.setQueryData(
        queryKeys.continueWatching,
        (old: unknown) => {
          if (!Array.isArray(old)) return old;
          return old.filter((it) => (it as { id?: string }).id !== itemId);
        },
      );
      return { previous };
    },
    onError: (_err, _itemId, ctx) => {
      if (ctx?.previous !== undefined) {
        queryClient.setQueryData(queryKeys.continueWatching, ctx.previous);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
    },
  });
}
