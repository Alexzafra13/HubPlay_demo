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
  Library,
  ProfileSummary,
  ResetPasswordResponse,
  User,
  UserLibraryAccess,
  UserPermissions,
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
  // `grant_library_ids` opts the new account into the household
  // library matrix in the same request — the server validates each
  // id exists before creating the row and rejects the field outright
  // on profile creation (ADR-014).
  return useMutation<
    CreateUserResponse,
    Error,
    {
      username: string;
      password?: string;
      display_name?: string;
      role?: string;
      grant_library_ids?: string[];
    }
  >({
    mutationFn: (data) => api.createUser(data),
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      // The post-create grant fan-out lands rows in library_access
      // that the matrix UI reads through this key. Invalidate
      // proactively so the row's checkbox state is correct the next
      // time the admin opens the modal.
      if (vars.grant_library_ids && vars.grant_library_ids.length > 0) {
        queryClient.invalidateQueries({
          queryKey: ["users"],
          predicate: (q) => q.queryKey[0] === "users",
        });
      }
    },
  });
}

// Per-user library matrix — admin-only. Profile ids resolve to the
// parent server-side, and the response flags `is_inherited` so the
// caller can render a read-only "inherited from parent" view without
// re-querying.
export function useUserLibraryAccess(
  userId: string | null | undefined,
  options?: Partial<UseQueryOptions<UserLibraryAccess>>,
) {
  return useQuery<UserLibraryAccess>({
    queryKey: userId
      ? queryKeys.userLibraryAccess(userId)
      : ["users", "library-access", "disabled"],
    queryFn: () => api.getUserLibraryAccess(userId as string),
    // Disabled when no target is selected (modal closed). Prevents
    // the hook from firing on every render of the parent page.
    enabled: !!userId,
    ...options,
  });
}

// Personal IPTV library shortcut. Creates a livetv library + grants
// access only to the target user in one server transaction. The
// matrix-access cache for this user is invalidated so the new lib
// appears ticked the next time the admin opens the access modal, and
// the global `libraries` list refreshes so the new entry shows up at
// /admin/libraries.
export function useCreatePersonalIPTVLibrary() {
  const queryClient = useQueryClient();
  return useMutation<
    Library,
    Error,
    {
      userId: string;
      name: string;
      m3uUrl: string;
      epgUrl?: string;
      languageFilter?: string[];
      tlsInsecure?: boolean;
    }
  >({
    mutationFn: ({ userId, name, m3uUrl, epgUrl, languageFilter, tlsInsecure }) =>
      api.createPersonalIPTVLibrary(userId, {
        name,
        m3u_url: m3uUrl,
        epg_url: epgUrl || undefined,
        language_filter:
          languageFilter && languageFilter.length > 0 ? languageFilter : undefined,
        tls_insecure: tlsInsecure || undefined,
      }),
    onSuccess: (_lib, vars) => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.userLibraryAccess(vars.userId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

// Admin permission flags (migración 055). El hook de lectura cae al
// listado /users (que ya trae los flags por fila) — sólo refresca
// vía /users/{id}/permissions cuando hay un PUT que invalida.
export function useUserPermissions(userId: string, enabled = true) {
  return useQuery<UserPermissions>({
    queryKey: queryKeys.userPermissions(userId),
    queryFn: () => api.getUserPermissions(userId),
    enabled: enabled && !!userId,
  });
}

export function useSetUserPermissions() {
  const queryClient = useQueryClient();
  return useMutation<
    UserPermissions,
    Error,
    { userId: string; flags: Partial<UserPermissions> }
  >({
    mutationFn: ({ userId, flags }) => api.setUserPermissions(userId, flags),
    onSuccess: (_data, vars) => {
      // El PUT cambia flags que el listado /users muestra; tiramos
      // todo el subtree "users" para que la matriz se repinte con
      // los valores nuevos sin un GET extra.
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({
        queryKey: queryKeys.userPermissions(vars.userId),
      });
      // Si el target soy yo, /me también cambia (puede haber añadido
      // o quitado un permiso al requester en su propio detalle).
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}

export function useSetUserLibraryAccess() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { userId: string; libraryIds: string[] }>({
    mutationFn: ({ userId, libraryIds }) =>
      api.setUserLibraryAccess(userId, libraryIds),
    onSuccess: (_data, vars) => {
      // The matrix view this user just saved reads from
      // queryKeys.userLibraryAccess(...); invalidate that key so the
      // next open shows the post-PUT state. Also drop any profile-
      // scoped cache pointing at the same household (the GET endpoint
      // normalises profile ids server-side, but we can't enumerate
      // them client-side, so we wipe the whole `users` subtree).
      queryClient.invalidateQueries({
        queryKey: queryKeys.userLibraryAccess(vars.userId),
      });
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

// Sube imagen → backend resize 256x256 JPEG → DB. Invalidamos /me +
// /users + /me/profiles para que cualquier sitio que renderice el
// avatar (TopBar, lista admin, picker de perfil) refresque ya con
// la nueva URL (que cambia en cada upload, así que el navegador
// refetchea aunque tuviera cache).
export function useUploadMyAvatar() {
  const queryClient = useQueryClient();
  return useMutation<{ avatar_image_url: string }, Error, File>({
    mutationFn: (file) => api.uploadMyAvatar(file),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
    },
  });
}

// Borra el avatar subido (idempotente). Tras esto el frontend vuelve
// a renderizar el círculo de iniciales sobre color.
export function useDeleteMyAvatar() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: () => api.deleteMyAvatar(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
      queryClient.invalidateQueries({ queryKey: ["me", "profiles"] });
    },
  });
}
