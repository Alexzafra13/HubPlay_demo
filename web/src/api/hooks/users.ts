// User-management hooks (admin surface). Listing, creation and
// deletion. Login/logout live in `auth.ts` because they're tied to
// the current session, not the admin user table.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { User } from "../types";

export function useUsers(options?: Partial<UseQueryOptions<User[]>>) {
  return useQuery<User[]>({
    queryKey: queryKeys.users,
    queryFn: () => api.getUsers(),
    ...options,
  });
}

export function useCreateUser() {
  const queryClient = useQueryClient();
  return useMutation<
    User,
    Error,
    { username: string; password: string; display_name?: string; role?: string }
  >({
    mutationFn: (data) => api.createUser(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}

export function useDeleteUser() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.deleteUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}
