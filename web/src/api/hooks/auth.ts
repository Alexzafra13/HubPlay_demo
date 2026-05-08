// Auth hooks: login, logout, current user.
//
// `useLogout` clears the entire query cache on settle (success OR
// failure) — once the user has expressed intent to log out, residual
// data from the previous session must not survive even if the server
// call hangs or 500s.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { AuthResponse, MySession, User } from "../types";

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
// "Tus dispositivos" panel in Settings. 30 s refetchInterval so the
// list reflects new logins from another device without forcing the
// user to refresh the page; 30 s also matches the system-stats
// cadence so the polling doesn't add a new heartbeat to the app.
export function useMySessions(options?: Partial<UseQueryOptions<MySession[]>>) {
  return useQuery<MySession[]>({
    queryKey: ["me", "sessions"],
    queryFn: () => api.listMySessions(),
    refetchInterval: 30_000,
    ...options,
  });
}

// Revoke a single session. On success we invalidate the list so the
// row drops out of the UI immediately. If the operator just revoked
// their own current session, the API has already cleared the
// cookies server-side — the next /me request will 401 and route
// the user to /login, which is exactly what we want.
export function useRevokeMySession() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { sessionId: string }>({
    mutationFn: ({ sessionId }) => api.revokeMySession(sessionId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["me", "sessions"] });
    },
  });
}
