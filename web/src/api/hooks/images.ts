// Image management hooks: per-item images, available-from-providers
// list, and the four mutations (select / upload / set-primary / delete)
// plus a library-wide refresh.
//
// Every mutation invalidates the item-detail cache too — the chosen
// poster/backdrop URL lives there and a stale render would show the
// previous artwork until the user navigates away.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { AvailableImage, ImageInfo } from "../types";

export function useItemImages(
  itemId: string,
  options?: Partial<UseQueryOptions<ImageInfo[]>>,
) {
  return useQuery<ImageInfo[]>({
    queryKey: queryKeys.itemImages(itemId),
    queryFn: () => api.getItemImages(itemId),
    enabled: !!itemId,
    ...options,
  });
}

export function useAvailableImages(
  itemId: string,
  type?: string,
  options?: Partial<UseQueryOptions<AvailableImage[]>>,
) {
  return useQuery<AvailableImage[]>({
    queryKey: queryKeys.availableImages(itemId, type),
    queryFn: () => api.getAvailableImages(itemId, type),
    enabled: !!itemId,
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

export function useSelectImage() {
  const queryClient = useQueryClient();
  return useMutation<
    ImageInfo,
    Error,
    { itemId: string; type: string; url: string; width: number; height: number }
  >({
    mutationFn: ({ itemId, type, ...data }) =>
      api.selectImage(itemId, type, data),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useUploadImage() {
  const queryClient = useQueryClient();
  return useMutation<
    ImageInfo,
    Error,
    { itemId: string; type: string; file: File }
  >({
    mutationFn: ({ itemId, type, file }) => api.uploadImage(itemId, type, file),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useSetImagePrimary() {
  const queryClient = useQueryClient();
  return useMutation<ImageInfo, Error, { itemId: string; imageId: string }>({
    mutationFn: ({ itemId, imageId }) => api.setImagePrimary(itemId, imageId),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useDeleteImage() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { itemId: string; imageId: string }>({
    mutationFn: ({ itemId, imageId }) => api.deleteImage(itemId, imageId),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useRefreshLibraryImages() {
  const queryClient = useQueryClient();
  return useMutation<{ updated: number }, Error, { libraryId: string }>({
    mutationFn: ({ libraryId }) => api.refreshLibraryImages(libraryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["libraries"] });
    },
  });
}
