// System status + signing-key admin.
//
// `useHealth` is the lightweight liveness probe; `useSystemStats` is
// the rich admin dashboard query. Auth-key management lives here too
// because rotation is also an admin-only system surface; semantically
// closer to "system" than to "auth flow".

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { useCallback } from "react";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import { useEventStream } from "@/hooks/useEventStream";
import type {
  AdminStreamActivityResponse,
  AdminStreamSession,
  AdminTopItemsResponse,
  AuthKey,
  HealthResponse,
  RotateAuthKeyResponse,
  SystemSettingsResponse,
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

// Per-day watch-activity rollup powering the Resumen sparkline. Ten-
// minute stale window: the data only changes when someone presses
// play, and refetch on tab focus already covers an admin returning
// from a different surface.
export function useAdminStreamActivity(
  days = 14,
  options?: Partial<UseQueryOptions<AdminStreamActivityResponse>>,
) {
  return useQuery<AdminStreamActivityResponse>({
    queryKey: ["admin", "stream-activity", days],
    queryFn: () => api.getAdminStreamActivity(days),
    staleTime: 10 * 60 * 1000,
    ...options,
  });
}

// Top-watched leaderboard powering the Resumen "Más visto" panel.
// Same staleTime rationale as stream activity.
export function useAdminTopItems(
  days = 7,
  limit = 5,
  options?: Partial<UseQueryOptions<AdminTopItemsResponse>>,
) {
  return useQuery<AdminTopItemsResponse>({
    queryKey: ["admin", "top-items", days, limit],
    queryFn: () => api.getAdminTopItems(days, limit),
    staleTime: 10 * 60 * 1000,
    ...options,
  });
}

// Runtime settings (server.base_url, hardware_acceleration.*) editable
// from the System panel. Mutations invalidate both the settings query
// and the stats one — `effective base_url` lives in stats too, and we
// want a save to make the new value visible without waiting on the
// 30 s stats refetch.
export function useSystemSettings(
  options?: Partial<UseQueryOptions<SystemSettingsResponse>>,
) {
  return useQuery<SystemSettingsResponse>({
    queryKey: queryKeys.systemSettings,
    queryFn: () => api.getSystemSettings(),
    ...options,
  });
}

export function useUpdateSystemSetting() {
  const qc = useQueryClient();
  return useMutation<
    SystemSettingsResponse,
    Error,
    { key: string; value: string }
  >({
    mutationFn: ({ key, value }) => api.updateSystemSetting(key, value),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.systemSettings, data);
      qc.invalidateQueries({ queryKey: queryKeys.systemStats });
    },
  });
}

export function useResetSystemSetting() {
  const qc = useQueryClient();
  return useMutation<SystemSettingsResponse, Error, { key: string }>({
    mutationFn: ({ key }) => api.resetSystemSetting(key),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.systemSettings, data);
      qc.invalidateQueries({ queryKey: queryKeys.systemStats });
    },
  });
}

// Active stream sessions for the admin "Now Playing" panel.
//
// Previously polled every 5 s — the most aggressive poll in the app,
// hammering the manager mutex 12×/min per admin viewer. Now driven by
// /events: the stream manager publishes transcode.started /
// transcode.completed on every session start/stop (the event names
// are legacy — DirectPlay bypasses the manager entirely, so the
// events cover the same set of sessions the list endpoint returns).
//
// Elapsed-time display in the panel still needs a ticker — see
// NowPlayingPanel's useNowTick for the 1 Hz re-render that keeps
// "started 23s ago" climbing without any network traffic.
export function useAdminStreamSessions(
  options?: Partial<UseQueryOptions<AdminStreamSession[]>>,
) {
  const qc = useQueryClient();
  const invalidate = useCallback(() => {
    qc.invalidateQueries({ queryKey: queryKeys.adminStreamSessions });
  }, [qc]);
  useEventStream("transcode.started", invalidate);
  useEventStream("transcode.completed", invalidate);
  return useQuery<AdminStreamSession[]>({
    queryKey: queryKeys.adminStreamSessions,
    queryFn: () => api.listAdminStreamSessions(),
    ...options,
  });
}

// Kill a session (admin scope). Optimistically nudges the local
// cache to remove the row before the next 5s poll lands, so the
// panel responds instantly to the click. The server's StopSession
// is idempotent, so a stale optimistic remove combined with a real
// kill via another admin tab is harmless.
export function useKillAdminStreamSession() {
  const qc = useQueryClient();
  return useMutation<void, Error, { sessionID: string }>({
    mutationFn: ({ sessionID }) => api.killAdminStreamSession(sessionID),
    onSuccess: (_data, vars) => {
      qc.setQueryData<AdminStreamSession[]>(queryKeys.adminStreamSessions, (prev) =>
        prev ? prev.filter((s) => s.session_id !== vars.sessionID) : prev,
      );
      qc.invalidateQueries({ queryKey: queryKeys.adminStreamSessions });
      // The system stats panel renders an "active sessions" gauge;
      // killing one should refresh that count without waiting on the
      // 30s system-stats refetch.
      qc.invalidateQueries({ queryKey: queryKeys.systemStats });
    },
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
