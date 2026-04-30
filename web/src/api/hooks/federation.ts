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
