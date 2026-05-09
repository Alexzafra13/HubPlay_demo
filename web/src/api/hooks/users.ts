// User-management hooks (admin surface). Listing, creation,
// deletion, and admin password reset. Login/logout live in
// `auth.ts` because they're tied to the current session, not the
// admin user table.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  CreateUserResponse,
  ProfileSummary,
  ResetPasswordResponse,
  User,
} from "../types";

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

// "Who's watching?" — the profile tree under the current account.
// 5-min staleTime: profiles change rarely and the create-profile
// mutation invalidates this key on success. We refetch on mount so
// the picker always reflects the freshest tree even when the cache
// holds a stale list (e.g. admin just added a profile in another
// tab and the user navigated to /select-profile).
export function useProfiles(options?: Partial<UseQueryOptions<ProfileSummary[]>>) {
  return useQuery<ProfileSummary[]>({
    queryKey: ["me", "profiles"],
    queryFn: () => api.listProfiles(),
    staleTime: 5 * 60 * 1000,
    refetchOnMount: "always",
    ...options,
  });
}

export function useSwitchProfile() {
  const queryClient = useQueryClient();
  return useMutation<
    { user: User; profiles?: ProfileSummary[] },
    Error,
    { profileId: string; pin?: string }
  >({
    mutationFn: ({ profileId, pin }) => api.switchProfile(profileId, pin),
    onSuccess: () => {
      // Switching changes the JWT identity → wipe every per-user
      // cache so the new profile sees its own user_data, not the
      // previous user's Continue Watching / Favorites / etc.
      queryClient.clear();
    },
  });
}

export function useCreateProfile() {
  const queryClient = useQueryClient();
  return useMutation<
    CreateUserResponse,
    Error,
    { parentUserId: string; displayName: string }
  >({
    mutationFn: ({ parentUserId, displayName }) =>
      api.createProfile(parentUserId, displayName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
    },
  });
}

export function useSetUserPIN() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; pin: string }>({
    mutationFn: ({ userId, pin }) => api.setUserPIN(userId, pin),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
    },
  });
}

export function useSetUserContentRating() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; rating: string }>({
    mutationFn: ({ userId, rating }) => api.setUserContentRating(userId, rating),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
    },
  });
}

// Rename a user / profile. Invalidates both the admin users list and
// the per-account profile tree so the new label appears everywhere
// (admin table + Who's-watching picker + topbar avatar) without a
// round-trip wait.
export function useSetUserDisplayName() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; displayName: string }>({
    mutationFn: ({ userId, displayName }) =>
      api.setUserDisplayName(userId, displayName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}

// Set / clear the avatar colour override. Same surfaces invalidated
// as the rename mutation since the avatar appears in all three.
export function useSetUserAvatarColor() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; hex: string }>({
    mutationFn: ({ userId, hex }) => api.setUserAvatarColor(userId, hex),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}

export function useSetUserRole() {
  const queryClient = useQueryClient();
  return useMutation<
    void,
    Error,
    { userId: string; role: "user" | "admin" }
  >({
    mutationFn: ({ userId, role }) => api.setUserRole(userId, role),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}

export function useSetUserActive() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; isActive: boolean }>({
    mutationFn: ({ userId, isActive }) => api.setUserActive(userId, isActive),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}

export function useSetUserAccess() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; durationDays: number }>({
    mutationFn: ({ userId, durationDays }) => api.setUserAccess(userId, durationDays),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}
