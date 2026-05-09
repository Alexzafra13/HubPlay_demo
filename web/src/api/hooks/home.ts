// Hooks for the configurable home page.
//
// The Home shell only needs three queries (layout + trending +
// live-now) plus one mutation (save layout). Per-library "latest"
// rails reuse the existing `useLatestItems(libraryId)` hook from
// media.ts — no new endpoint required there.

import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  HomeLayout,
  HomeLiveNowChannel,
  HomeBecauseResponse,
  HomeRecommendedItem,
  HomeTrendingItem,
} from "../types";

export function useHomeLayout(
  options?: Partial<UseQueryOptions<HomeLayout>>,
) {
  return useQuery<HomeLayout>({
    queryKey: queryKeys.homeLayout,
    queryFn: () => api.getHomeLayout(),
    // The layout document is small and rarely changes — keep it
    // fresh longer than catalog data, since a user toggling a rail
    // expects an immediate refetch via the mutation invalidation
    // anyway.
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

export function usePutHomeLayout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (layout: HomeLayout) => api.putHomeLayout(layout),
    onSuccess: (saved) => {
      qc.setQueryData(queryKeys.homeLayout, saved);
    },
  });
}

export function useHomeTrending(
  options?: Partial<UseQueryOptions<HomeTrendingItem[]>>,
) {
  return useQuery<HomeTrendingItem[]>({
    queryKey: queryKeys.homeTrending,
    queryFn: () => api.getHomeTrending(),
    // Trending is a server-wide aggregate over a 7-day window.
    // It moves slowly — five-minute stale window keeps the rail
    // stable across navigations without hammering the DB.
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

export function useHomeRecommended(
  options?: Partial<UseQueryOptions<HomeRecommendedItem[]>>,
) {
  return useQuery<HomeRecommendedItem[]>({
    queryKey: queryKeys.homeRecommended,
    queryFn: () => api.getHomeRecommended(),
    // Recommendations are derived from the user's genre affinity,
    // which only shifts when they finish (or significantly start)
    // something new. A five-minute stale window matches Trending,
    // and the home page refetches anyway when the layout changes.
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

// "Porque viste X" rail. Returns the seed (latest completed watch)
// + recommendations sharing genres with it. Same staleTime as
// Recommended (5 min) since the seed only flips when the user
// finishes another item; refetchOnMount stays default ("if stale")
// so re-entering Home from a navigation doesn't fire an extra
// request mid-session.
export function useHomeBecauseYouWatched(
  options?: Partial<UseQueryOptions<HomeBecauseResponse>>,
) {
  return useQuery<HomeBecauseResponse>({
    queryKey: queryKeys.homeBecauseYouWatched,
    queryFn: () => api.getHomeBecauseYouWatched(),
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

export function useHomeLiveNow(
  options?: Partial<UseQueryOptions<HomeLiveNowChannel[]>>,
) {
  return useQuery<HomeLiveNowChannel[]>({
    queryKey: queryKeys.homeLiveNow,
    queryFn: () => api.getHomeLiveNow(),
    // EPG slots flip every ~30 min in the worst case (most are
    // longer). 60s is generous; the user's manual navigation back
    // to Home will always pick up the latest anyway.
    staleTime: 60 * 1000,
    refetchInterval: 60 * 1000,
    ...options,
  });
}
