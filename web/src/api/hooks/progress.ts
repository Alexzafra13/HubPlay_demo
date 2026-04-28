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
