// User-management hooks (admin surface). Listing, creation,
// deletion, and admin password reset. Login/logout live in
// `auth.ts` because they're tied to the current session, not the
// admin user table.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { CreateUserResponse, ResetPasswordResponse, User } from "../types";

export function useUsers(options?: Partial<UseQueryOptions<User[]>>) {
  return useQuery<User[]>({
    queryKey: queryKeys.users,
    queryFn: () => api.getUsers(),
    ...options,
  });
}

export function useCreateUser() {
  const queryClient = useQueryClient();
  // Password is now optional from the wire perspective: omit it and
  // the server generates a temporary one and returns it under
  // `generated_password` for the admin to share with the user.
  return useMutation<
    CreateUserResponse,
    Error,
    { username: string; password?: string; display_name?: string; role?: string }
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

// Admin reset. Returns the generated temporary password exactly
// once — the admin pane copies it into a "share with user" modal.
// On success we invalidate the users list so the row's
// password_change_required flag flips immediately in the table.
export function useResetUserPassword() {
  const queryClient = useQueryClient();
  return useMutation<ResetPasswordResponse, Error, string>({
    mutationFn: (id) => api.resetUserPassword(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}

// Self password change. Caller passes the current password (or empty
// when completing a forced rotation) and the new one. On success we
// invalidate `me` so the cached `password_change_required` flips off
// without an extra round-trip.
export function useChangeMyPassword() {
  const queryClient = useQueryClient();
  return useMutation<
    void,
    Error,
    { currentPassword: string; newPassword: string }
  >({
    mutationFn: ({ currentPassword, newPassword }) =>
      api.changeMyPassword(currentPassword, newPassword),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}
