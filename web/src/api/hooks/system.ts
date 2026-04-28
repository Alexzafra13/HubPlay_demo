// System status + signing-key admin.
//
// `useHealth` is the lightweight liveness probe; `useSystemStats` is
// the rich admin dashboard query. Auth-key management lives here too
// because rotation is also an admin-only system surface; semantically
// closer to "system" than to "auth flow".

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  AuthKey,
  HealthResponse,
  RotateAuthKeyResponse,
  SystemStats,
} from "../types";

export function useHealth(options?: Partial<UseQueryOptions<HealthResponse>>) {
  return useQuery<HealthResponse>({
    queryKey: queryKeys.health,
    queryFn: () => api.getHealth(),
    ...options,
  });
}

// Rich admin-only system stats. Polled by the System admin page; defaults
// to no auto-refresh so the caller decides the cadence (the page passes
// refetchInterval to keep the data live without flicker).
export function useSystemStats(options?: Partial<UseQueryOptions<SystemStats>>) {
  return useQuery<SystemStats>({
    queryKey: queryKeys.systemStats,
    queryFn: () => api.getSystemStats(),
    ...options,
  });
}

// Signing-key management for the admin panel.
//
// The list query is light (in-memory snapshot) so we let it refetch on
// focus, but it's not on a timer — rotations are admin-driven, not
// automatic, so polling adds no value.
export function useAuthKeys(options?: Partial<UseQueryOptions<AuthKey[]>>) {
  return useQuery<AuthKey[]>({
    queryKey: queryKeys.authKeys,
    queryFn: () => api.listAuthKeys(),
    ...options,
  });
}

export function useRotateAuthKey() {
  const queryClient = useQueryClient();
  return useMutation<
    RotateAuthKeyResponse,
    Error,
    { overlapSeconds?: number } | void
  >({
    mutationFn: (vars) => api.rotateAuthKey(vars?.overlapSeconds),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.authKeys });
    },
  });
}

export function usePruneAuthKeys() {
  const queryClient = useQueryClient();
  return useMutation<
    { pruned: number },
    Error,
    { beforeSeconds?: number } | void
  >({
    mutationFn: (vars) => api.pruneAuthKeys(vars?.beforeSeconds),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.authKeys });
    },
  });
}
