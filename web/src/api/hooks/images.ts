// Image management hooks: per-item images, available-from-providers
// list, and the four mutations (select / upload / set-primary / delete)
// plus a library-wide refresh.
//
// Every mutation invalidates:
//   - itemImages(itemId) — la lista en el panel del ImageManager.
//   - item(itemId)       — el JSON del detail (poster_url, etc).
//   - ["items"]          — TODOS los listados que enseñan posters:
//                          /movies, /series, latest, continue-watching,
//                          recomendados, etc. Sin esto, el grid de
//                          home seguía mostrando el póster viejo
//                          hasta que el usuario recargaba la página.
//
// Tampoco basta con invalidar el query: si el backend devuelve la
// MISMA URL para la nueva imagen (porque el poster_url no cambia
// cuando se reemplaza el contenido), el navegador sirve la versión
// cacheada. Por eso bumpeamos un nonce global que los componentes
// consumen vía useImageCacheNonce() y lo concatenan al src.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { create } from "zustand";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { AvailableImage, ImageInfo } from "../types";

// Cache-buster global para imágenes. Cualquier mutación de imagen
// bumpea el nonce; los <img> que dependen de él (ImageManager,
// PosterCard si lo necesita) lo concatenan como query param y el
// browser refetchea sin que tengamos que cambiar el backend.
interface ImageCacheNonceState {
  nonce: number;
  bump: () => void;
}
const useImageCacheNonceStore = create<ImageCacheNonceState>((set) => ({
  nonce: 0,
  bump: () => set((s) => ({ nonce: s.nonce + 1 })),
}));
export const useImageCacheNonce = () =>
  useImageCacheNonceStore((s) => s.nonce);

function invalidateImageQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  itemId: string,
) {
  queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
  queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
  // Broad: los listados (movies/series/latest/continue-watching/...)
  // todos viven bajo ["items", ...]. Invalidamos el namespace entero
  // — el coste de un refetch ocasional es trivial frente al sufrimiento
  // del usuario viendo el póster equivocado.
  queryClient.invalidateQueries({ queryKey: ["items"] });
  queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
  queryClient.invalidateQueries({ queryKey: queryKeys.homeRecommended });
  queryClient.invalidateQueries({ queryKey: queryKeys.homeTrending });
  // Y bumpeamos el nonce para que los <img> hagan refetch real.
  useImageCacheNonceStore.getState().bump();
}

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
      invalidateImageQueries(queryClient, itemId);
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
      invalidateImageQueries(queryClient, itemId);
    },
  });
}

export function useSetImagePrimary() {
  const queryClient = useQueryClient();
  return useMutation<ImageInfo, Error, { itemId: string; imageId: string }>({
    mutationFn: ({ itemId, imageId }) => api.setImagePrimary(itemId, imageId),
    onSuccess: (_data, { itemId }) => {
      invalidateImageQueries(queryClient, itemId);
    },
  });
}

export function useDeleteImage() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { itemId: string; imageId: string }>({
    mutationFn: ({ itemId, imageId }) => api.deleteImage(itemId, imageId),
    onSuccess: (_data, { itemId }) => {
      invalidateImageQueries(queryClient, itemId);
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
