// Federation admin hooks. The federation feature lets two HubPlay
// servers pair so users on either side can browse / play each other's
// content — see docs/architecture/federation.md.
//
// Backend: internal/federation/manager.go + handlers/federation_admin.go.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  FederationInvite,
  FederationPeer,
  FederationServerInfo,
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
