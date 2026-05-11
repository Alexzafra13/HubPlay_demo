// Auth hooks: login, logout, current user.
//
// `useLogout` clears the entire query cache on settle (success OR
// failure) — once the user has expressed intent to log out, residual
// data from the previous session must not survive even if the server
// call hangs or 500s.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { useCallback } from "react";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { AuthResponse, MySession, User } from "../types";
import { useUserEventStream } from "@/hooks/useUserEventStream";

const MY_SESSIONS_QUERY_KEY = ["me", "sessions"] as const;

export function useMe(options?: Partial<UseQueryOptions<User>>) {
  return useQuery<User>({
    queryKey: queryKeys.me,
    queryFn: () => api.getMe(),
    ...options,
  });
}

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation<AuthResponse, Error, { username: string; password: string }>({
    mutationFn: ({ username, password }) => api.login(username, password),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: () => api.logout(),
    onSettled: () => {
      // Always clear cache and redirect, even if the API call fails.
      // The user wants to log out regardless of server response.
      queryClient.clear();
    },
  });
}

// Active auth sessions for the calling user — drives the
// "Tus dispositivos" panel in Settings.
//
// Previously polled every 30 s. Now driven by /me/events: the backend
// publishes user.logged_in (new device authenticated) and
// user.logged_out (Logout or RevokeSession) filtered to this user,
// so the list reflects within ~50 ms instead of up to 30 s. The
// server-side filter (me_events.go) only forwards events whose
// Data.user_id matches the authenticated user, so other households
// can't peek at each other's session activity through this channel.
export function useMySessions(options?: Partial<UseQueryOptions<MySession[]>>) {
  const queryClient = useQueryClient();
  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: MY_SESSIONS_QUERY_KEY });
  }, [queryClient]);
  useUserEventStream("user.logged_in", invalidate);
  useUserEventStream("user.logged_out", invalidate);
  return useQuery<MySession[]>({
    queryKey: MY_SESSIONS_QUERY_KEY,
    queryFn: () => api.listMySessions(),
    ...options,
  });
}

// Revoke a single session. On success we invalidate the list so the
// row drops out of the UI immediately. If the operator just revoked
// their own current session, the API has already cleared the
// cookies server-side — the next /me request will 401 and route
// the user to /login, which is exactly what we want.
//
// The backend also publishes user.logged_out on revoke, so the
// invalidate below is technically redundant with the SSE-driven
// one in useMySessions — kept for fast feedback on the originating
// tab (the SSE round-trip is the slow path and we want the row to
// disappear before the next paint).
export function useRevokeMySession() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { sessionId: string }>({
    mutationFn: ({ sessionId }) => api.revokeMySession(sessionId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: MY_SESSIONS_QUERY_KEY });
    },
  });
}
