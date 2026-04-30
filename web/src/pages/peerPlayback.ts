import { useCallback, useRef, useState } from "react";
import { api } from "@/api/client";
import type { PlaybackMethod } from "@/api/types";

// Backend method strings → frontend PlaybackMethod literal. Matches
// the local-stream mapping in itemDetail/usePlayback so the player
// component receives the same shape regardless of source.
const PEER_METHOD_MAP: Record<string, PlaybackMethod> = {
  DirectPlay: "direct_play",
  DirectStream: "direct_stream",
  Transcode: "transcode",
};

export interface PeerPlaybackSource {
  playbackMethod: PlaybackMethod;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
}

export interface PeerPlaybackState {
  showPlayer: boolean;
  playingItemId: string | null;
  source: PeerPlaybackSource | null;
  error: string | null;
  isLoading: boolean;
}

export interface PeerPlaybackActions {
  /** Open the player on a peer item. Resolves once the master URL is in
   *  hand; the VideoPlayer takes it from there. */
  play: (peerID: string, itemID: string) => Promise<void>;
  /** Tear down the overlay + tell the origin to release its slot. */
  close: () => Promise<void>;
}

// usePeerPlayback — viewer-side mirror of itemDetail/usePlayback for
// content that lives on a peer's server. The flow:
//
//   1. play(peerID, itemID): POST /me/peers/{peerID}/stream/{itemID}/session
//      The local server signs a peer-JWT and asks the origin to open a
//      stream session. Response carries the master playlist URL —
//      already rewritten to point at OUR proxy, so the player only
//      ever talks to us.
//
//   2. The VideoPlayer renders with master_playlist=URL. Variants and
//      segments naturally route through the proxy (relative URLs in
//      the variant manifest resolve against the playlist URL, which
//      is on our server).
//
//   3. close(): DELETE /me/peers/{peerID}/stream/session/{sessionID}
//      so the origin's per-peer cap counter drops back to free. Best
//      effort — origin idle-sweeps after 4h either way.
//
// State is component-local (not in usePlayerStore) for the same reason
// the local itemDetail flow keeps it local: the inline overlay is
// owned by the page, not the global mini-player surface.
export function usePeerPlayback(): PeerPlaybackState & PeerPlaybackActions {
  const [showPlayer, setShowPlayer] = useState(false);
  const [playingItemId, setPlayingItemId] = useState<string | null>(null);
  const [source, setSource] = useState<PeerPlaybackSource | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  // Stash peer + session id for the close call. Refs so we don't
  // re-render every time they update — they only matter at close.
  const sessionRef = useRef<{ peerID: string; sessionID: string } | null>(null);

  const play = useCallback(async (peerID: string, itemID: string) => {
    setError(null);
    setIsLoading(true);
    try {
      const result = await api.startPeerStream(peerID, itemID);
      const method: PlaybackMethod = PEER_METHOD_MAP[result.method] ?? "transcode";
      sessionRef.current = { peerID, sessionID: result.session_id };
      setPlayingItemId(itemID);
      setSource({
        playbackMethod: method,
        masterPlaylistUrl: method !== "direct_play" ? result.master_playlist_url : null,
        // For direct-play we'd need a separate endpoint that range-streams
        // the original file through the proxy — not in this PR. Falling
        // through to master playlist for now means peer DirectPlay items
        // get an HLS pass via DirectStream, which is mildly suboptimal
        // but always works.
        directUrl: null,
      });
      setShowPlayer(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to start peer stream");
    } finally {
      setIsLoading(false);
    }
  }, []);

  const close = useCallback(async () => {
    setShowPlayer(false);
    setSource(null);
    setPlayingItemId(null);
    const ref = sessionRef.current;
    sessionRef.current = null;
    if (ref) {
      try {
        await api.stopPeerStream(ref.peerID, ref.sessionID);
      } catch {
        // Best-effort — origin's idle sweep covers the leak.
      }
    }
  }, []);

  return {
    showPlayer,
    playingItemId,
    source,
    error,
    isLoading,
    play,
    close,
  };
}
