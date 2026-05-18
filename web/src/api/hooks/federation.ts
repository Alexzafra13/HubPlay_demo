// Federation admin hooks. The federation feature lets two HubPlay
// servers pair so users on either side can browse / play each other's
// content — see docs/architecture/federation.md.
//
// Backend: internal/federation/manager.go + handlers/federation_admin.go.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  FederationConnectedPeer,
  FederationInvite,
  FederationLibraryShare,
  FederationPeer,
  FederationRemoteItemsResponse,
  FederationRemoteLibrary,
  FederationSearchResponse,
  FederationServerInfo,
  FederationUnifiedLibrary,
} from "../types";

// useServerIdentity returns this server's own ServerInfo so the admin
// can read their fingerprint to a remote admin out-of-band during
// handshake confirmation.
export function useServerIdentity() {
  return useQuery<FederationServerInfo>({
    queryKey: queryKeys.federationIdentity,
    queryFn: () => api.getServerIdentity(),
    // Server identity is stable for the life of the server (until key
    // rotation in Phase 2+). Cache aggressively.
    staleTime: 30 * 60 * 1000, // 30 min
  });
}

// useUpdateServerIdentity persiste el nombre visible + color hex
// del avatar del servidor desde el panel de Federation. Invalida la
// cache de identity para que IdentityCard repinte con los valores
// nuevos sin tener que refrescar la pagina.
export function useUpdateServerIdentity() {
  const queryClient = useQueryClient();
  return useMutation<
    FederationServerInfo,
    Error,
    { name: string; avatarColor: string }
  >({
    mutationFn: ({ name, avatarColor }) =>
      api.updateServerIdentity({ name, avatar_color: avatarColor }),
    onSuccess: (info) => {
      queryClient.setQueryData(queryKeys.federationIdentity, info);
    },
  });
}

// useUploadServerAvatar sube la foto del servidor. El backend
// devuelve el ServerInfo entero (mismo shape que update), así que
// pisamos directamente la cache de useServerIdentity para que la
// UI repinte sin round-trip extra.
export function useUploadServerAvatar() {
  const queryClient = useQueryClient();
  return useMutation<FederationServerInfo, Error, File>({
    mutationFn: (file) => api.uploadServerAvatar(file),
    onSuccess: (info) => {
      queryClient.setQueryData(queryKeys.federationIdentity, info);
    },
  });
}

// useDeleteServerAvatar quita la foto. Idempotente. Invalidamos
// la cache de identity para que IdentityCard refresque el avatar
// (que ahora cae al color/iniciales).
export function useDeleteServerAvatar() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: () => api.deleteServerAvatar(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.federationIdentity });
    },
  });
}

export function usePeers() {
  return useQuery<FederationPeer[]>({
    queryKey: queryKeys.federationPeers,
    queryFn: () => api.listPeers(),
  });
}

export function useGenerateInvite() {
  const queryClient = useQueryClient();
  return useMutation<FederationInvite, Error>({
    mutationFn: () => api.generateInvite(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.federationInvites });
    },
  });
}

export function useListInvites() {
  return useQuery<FederationInvite[]>({
    queryKey: queryKeys.federationInvites,
    queryFn: () => api.listInvites(),
  });
}

// useProbePeer — non-mutating; we use mutation flavour because the
// admin triggers it explicitly per attempt rather than on mount, and
// useMutation gives us mutateAsync + isPending which fits the
// "click probe button → wait → show result" UX cleanly.
export function useProbePeer() {
  return useMutation<FederationServerInfo, Error, string>({
    mutationFn: (baseURL) => api.probePeer(baseURL),
  });
}

export function useAcceptInvite() {
  const queryClient = useQueryClient();
  return useMutation<FederationPeer, Error, { baseURL: string; code: string }>({
    mutationFn: ({ baseURL, code }) => api.acceptInvite(baseURL, code),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.federationPeers });
      queryClient.invalidateQueries({ queryKey: queryKeys.federationInvites });
    },
  });
}

export function useRevokePeer() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.revokePeer(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.federationPeers });
    },
  });
}

// usePeerShares lists every library share row attached to a peer.
// Powers the per-peer expansion panel in FederationAdmin.
export function usePeerShares(peerID: string, enabled = true) {
  return useQuery<FederationLibraryShare[]>({
    queryKey: queryKeys.federationPeerShares(peerID),
    queryFn: () => api.listPeerShares(peerID),
    enabled: enabled && Boolean(peerID),
  });
}

// useCreatePeerShare upserts a share — idempotent on (peer, library).
// On success invalidates the per-peer shares query so the UI reflects
// the new scope set immediately.
export function useCreatePeerShare(peerID: string) {
  const queryClient = useQueryClient();
  return useMutation<
    FederationLibraryShare,
    Error,
    {
      libraryID: string;
      canBrowse: boolean;
      canPlay: boolean;
      canDownload: boolean;
      canLiveTV: boolean;
    }
  >({
    mutationFn: (vars) =>
      api.createPeerShare(peerID, {
        library_id: vars.libraryID,
        can_browse: vars.canBrowse,
        can_play: vars.canPlay,
        can_download: vars.canDownload,
        can_livetv: vars.canLiveTV,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.federationPeerShares(peerID) });
    },
  });
}

export function useDeletePeerShare(peerID: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (shareID) => api.deletePeerShare(peerID, shareID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.federationPeerShares(peerID) });
    },
  });
}

// ─── User-facing browsing (Phase 4) ────────────────────────────────

// useMyPeers — paired peers visible to the current user.
export function useMyPeers() {
  return useQuery<FederationConnectedPeer[]>({
    queryKey: queryKeys.myPeers,
    queryFn: () => api.listMyPeers(),
  });
}

// useAllPeerLibraries — flat list of (library × peer) across every
// paired peer in one round trip. Powers the unified /peers landing
// page so the user sees all available libraries at a glance, not
// nested peer-then-library navigation.
export function useAllPeerLibraries() {
  return useQuery<FederationUnifiedLibrary[]>({
    queryKey: queryKeys.myPeerLibrariesUnified,
    queryFn: () => api.listAllPeerLibraries(),
  });
}

// usePeerLibraries — libraries a specific peer has shared with us.
// Live every time (no cache layer for libraries — it's a small list).
export function usePeerLibraries(peerID: string, enabled = true) {
  return useQuery<FederationRemoteLibrary[]>({
    queryKey: queryKeys.myPeerLibraries(peerID),
    queryFn: () => api.browsePeerLibraries(peerID),
    enabled: enabled && Boolean(peerID),
  });
}

// usePeerItems — paginated items in a peer's library. Reads through
// the catalog cache server-side; the response carries a from_cache
// flag the UI uses for the freshness badge.
export function usePeerItems(peerID: string, libraryID: string, offset = 0, limit = 50) {
  return useQuery<FederationRemoteItemsResponse>({
    queryKey: queryKeys.myPeerItems(peerID, libraryID, offset),
    queryFn: () => api.browsePeerItems(peerID, libraryID, { offset, limit }),
    enabled: Boolean(peerID && libraryID),
  });
}

// usePeersSearch — federated full-text search across every paired
// peer. Runs in parallel with the local /items/search query so the
// page renders local hits immediately and merges peer hits as they
// arrive. The backend caps per-peer wait at ~2s and silently skips
// peers that error / time out, so this query is much closer to "best
// effort" than the local search and may legitimately return zero
// hits with status=success when no peer answered in time.
export function usePeersSearch(
  q: string,
  enabled = true,
) {
  return useQuery<FederationSearchResponse>({
    queryKey: queryKeys.myPeersSearch(q),
    queryFn: () => api.searchPeers(q),
    // Trigger only when the user actually typed something. The Search
    // page already debounces and trims the query before passing it
    // in, so any non-empty string here is one we want to send.
    enabled: enabled && q.length > 0,
  });
}

// usePeerRecent — federated "Recently added on peers" rail. Server
// fans out to every paired peer in parallel with a per-peer timeout
// so a slow / offline peer can't block the rest. Same hit shape as
// the federated search response, so the rail reuses the existing
// FederationSearchHit type.
export function usePeerRecent(perPeerLimit = 12) {
  return useQuery<FederationSearchResponse>({
    queryKey: queryKeys.myPeersRecent,
    queryFn: () => api.getPeerRecent(perPeerLimit),
    // Recently-added moves on a slow tempo (new scans land minutes
    // apart at most). Cache for a minute so rapid Home re-renders
    // don't stampede the fan-out.
    staleTime: 60 * 1000,
  });
}

// useRefreshPeerLibrary — admin "force refresh" button. Purges the
// cache for (peer, library) so the next browse forces a live fetch.
export function useRefreshPeerLibrary(peerID: string, libraryID: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: () => api.refreshPeerLibrary(peerID, libraryID),
    onSuccess: () => {
      // Invalidate any cached pages for this library so the next view
      // re-fetches.
      queryClient.invalidateQueries({
        queryKey: ["me", "peers", peerID, "libraries", libraryID, "items"],
      });
    },
  });
}
