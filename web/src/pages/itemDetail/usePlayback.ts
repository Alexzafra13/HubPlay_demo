import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import { queryKeys } from "@/api/hooks";
import type { MediaItem, PlaybackMethod } from "@/api/types";

// HLS protocol map shared between initial play and auto-advance.
const PLAYBACK_METHOD_MAP: Record<string, PlaybackMethod> = {
  DirectPlay: "direct_play",
  DirectStream: "direct_stream",
  Transcode: "transcode",
};

interface PlayerSourceInfo {
  playbackMethod: PlaybackMethod;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
}

// resolvePlayerSource hits /stream/:id/info and turns the response
// into the URL pair the VideoPlayer expects (master.m3u8 OR direct,
// never both). Pulled out so the initial-play and auto-advance flows
// share one canonical mapping.
async function resolvePlayerSource(itemId: string): Promise<PlayerSourceInfo> {
  const info = await api.getStreamInfo(itemId);
  const rawMethod = (info as Record<string, unknown>).method as string ?? "";
  const method: PlaybackMethod = PLAYBACK_METHOD_MAP[rawMethod] ?? "transcode";
  return {
    playbackMethod: method,
    masterPlaylistUrl:
      method !== "direct_play" ? `/api/v1/stream/${itemId}/master.m3u8` : null,
    directUrl: method === "direct_play" ? `/api/v1/stream/${itemId}/direct` : null,
  };
}

// Best-effort session teardown. Failures here are silent — the server
// has its own session reaper and a missed DELETE just sits idle until
// then. No user-visible behaviour depends on this resolving.
async function cleanupSession(itemId: string): Promise<void> {
  try {
    const token = localStorage.getItem("hubplay_access_token");
    await fetch(`/api/v1/stream/${itemId}/session`, {
      method: "DELETE",
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
  } catch {
    // Cleanup is best-effort; ignore network/auth errors.
  }
}

interface UsePlaybackArgs {
  /** The detail page's primary item id (URL param). Used as the
   *  default playback target when handlePlay is called without an
   *  explicit override. */
  pageItemId: string | undefined;
  /** Sibling episodes for the auto-advance pipeline. Empty for
   *  movies and orphan episodes. */
  siblingEpisodes: MediaItem[];
  /** Optional duration of the item in seconds (parent passes the
   *  primary item's duration_ticks → seconds conversion). The hook
   *  doesn't need it for state, only memoises chapter markers off
   *  the parent's chapters list — so the conversion stays at the
   *  call site, the hook accepts the seconds-shape directly. */
}

export interface PlayerOverlayState {
  showPlayer: boolean;
  playerInfo: PlayerSourceInfo | null;
  /** The item currently playing in the overlay. Diverges from
   *  pageItemId when the user clicks an episode row on a season
   *  detail page. */
  playingItemId: string | null;
  playError: string | null;
}

export interface NextUpInfo {
  title: string;
  seasonNumber: number | null | undefined;
  episodeNumber: number | null | undefined;
  posterUrl: string | null | undefined;
  backdropUrl: string | null | undefined;
}

export interface UsePlaybackResult extends PlayerOverlayState {
  /** Trigger inline playback. Pass an episode id to play that one
   *  instead of the page's default item (used by EpisodeRow on
   *  season pages). */
  handlePlay: (targetId?: string) => Promise<void>;
  /** Hooked into VideoPlayer.onEnded — finds the next sibling
   *  episode and advances to it without closing the overlay. */
  handlePlayerEnded: () => void;
  /** Hooked into VideoPlayer.onClose — tears the overlay down and
   *  fires a session DELETE so the backend transcoder isn't left
   *  buffering ahead for nothing. */
  handleClosePlayer: () => Promise<void>;
  /** Up-next promo card data for the player overlay. undefined when
   *  there's no next sibling. */
  nextUpInfo: NextUpInfo | undefined;
}

/**
 * usePlayback — owns the inline-player overlay lifecycle for the
 * item-detail page.
 *
 * Responsibilities:
 *   - Show/hide the VideoPlayer overlay.
 *   - Resolve the right HLS / direct URL for the chosen item.
 *   - Track which item is currently playing (may differ from the
 *     page's URL id when an episode row is clicked on a season page).
 *   - Auto-advance to the next sibling episode on `onEnded`.
 *   - Prefetch the next episode's detail so the up-next overlay has
 *     a warm cache.
 *   - DELETE the streaming session on close + on retarget so the
 *     backend transcoder doesn't keep producing segments for an
 *     overlay nobody is watching.
 *
 * Pulled out of `ItemDetail.tsx` because the playback state machine
 * is independent of the page's render: a future Live TV inline
 * surface, a mobile mini-player, or a continue-watching overlay
 * could reuse this hook unchanged.
 */
export function usePlayback({
  pageItemId,
  siblingEpisodes,
}: UsePlaybackArgs): UsePlaybackResult {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const [showPlayer, setShowPlayer] = useState(false);
  const [playerInfo, setPlayerInfo] = useState<PlayerSourceInfo | null>(null);
  const [playError, setPlayError] = useState<string | null>(null);
  const [playingItemId, setPlayingItemId] = useState<string | null>(null);
  const isPlayingRef = useRef(false);

  const cleanup = useCallback(async (itemId: string) => {
    await cleanupSession(itemId);
    isPlayingRef.current = false;
  }, []);

  const handlePlay = useCallback(
    async (targetId?: string) => {
      const playId = targetId ?? pageItemId;
      if (!playId) return;
      setPlayError(null);

      try {
        if (isPlayingRef.current && playingItemId) {
          await cleanup(playingItemId);
        }
        const source = await resolvePlayerSource(playId);
        isPlayingRef.current = true;
        setPlayingItemId(playId);
        setPlayerInfo(source);
        setShowPlayer(true);
      } catch {
        setPlayError(t("itemDetail.playbackError"));
      }
    },
    [pageItemId, playingItemId, cleanup, t],
  );

  // Next-episode lookup. Used both to prefetch its item data when
  // the current episode starts playing (warmer cache for the auto-
  // advance round-trip) and to feed the up-next overlay so it knows
  // what to promote when the current video ends.
  const nextEpisode = useMemo<MediaItem | undefined>(() => {
    if (!playingItemId || siblingEpisodes.length === 0) return undefined;
    const idx = siblingEpisodes.findIndex((ep) => ep.id === playingItemId);
    return idx >= 0 ? siblingEpisodes[idx + 1] : undefined;
  }, [playingItemId, siblingEpisodes]);

  useEffect(() => {
    if (!nextEpisode) return;
    queryClient.prefetchQuery({
      queryKey: queryKeys.item(nextEpisode.id),
      queryFn: () => api.getItem(nextEpisode.id),
      staleTime: 5 * 60 * 1000,
    });
  }, [nextEpisode, queryClient]);

  const nextUpInfo = useMemo<NextUpInfo | undefined>(() => {
    if (!nextEpisode) return undefined;
    return {
      title: nextEpisode.title,
      seasonNumber: nextEpisode.season_number,
      episodeNumber: nextEpisode.episode_number,
      posterUrl: nextEpisode.poster_url,
      backdropUrl: nextEpisode.backdrop_url,
    };
  }, [nextEpisode]);

  const handlePlayerEnded = useCallback(() => {
    if (!playingItemId || siblingEpisodes.length === 0) return;
    const idx = siblingEpisodes.findIndex((ep) => ep.id === playingItemId);
    const nextEp = idx >= 0 ? siblingEpisodes[idx + 1] : undefined;
    if (!nextEp) return;

    setPlayingItemId(nextEp.id);
    void (async () => {
      try {
        if (isPlayingRef.current && playingItemId) {
          await cleanup(playingItemId);
        }
        const source = await resolvePlayerSource(nextEp.id);
        isPlayingRef.current = true;
        setPlayerInfo(source);
      } catch {
        setShowPlayer(false);
        setPlayerInfo(null);
      }
    })();
  }, [playingItemId, siblingEpisodes, cleanup]);

  const handleClosePlayer = useCallback(async () => {
    setShowPlayer(false);
    setPlayerInfo(null);
    setPlayingItemId(null);
    if (playingItemId || pageItemId) {
      await cleanup(playingItemId || pageItemId!);
    }
  }, [pageItemId, playingItemId, cleanup]);

  return {
    showPlayer,
    playerInfo,
    playingItemId,
    playError,
    nextUpInfo,
    handlePlay,
    handlePlayerEnded,
    handleClosePlayer,
  };
}
