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
import type { AuthResponse, User } from "../types";

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
