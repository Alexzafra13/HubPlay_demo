import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import { queryKeys, useUserPreference } from "@/api/hooks";
import type { MediaItem, PlaybackMethod } from "@/api/types";
import {
  PREFERRED_AUDIO_LANG_PREF_KEY,
  pickAudioStreamIndex,
} from "@/utils/playbackPrefs";

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
  // Resolved audio stream index for the player's audio menu (lets
  // the in-player switcher mark the active option without having
  // to re-derive the language match).
  audioStreamIndex: number;
  // Per-type index of the subtitle currently being burned into the
  // video frames. -1 = no burn-in (the player is either off-subs or
  // riding a native HLS sub track). Carried in the URL as
  // ?subtitle=N so the manager session key + session-restart paths
  // pick up the same value. Drives the picker's "selected"
  // indicator for burn-in entries (PGS / DVDSUB / ASS).
  burnSubtitleIndex: number;
  // Seconds to seek to once the new manifest attaches. Set when
  // the user switches audio dub mid-playback so the next master
  // load resumes at the same playhead. Undefined for fresh plays.
  startPosition?: number;
}

// resolvePlayerSource hits /stream/:id/info and turns the response
// into the URL pair the VideoPlayer expects (master.m3u8 OR direct,
// never both). Pulled out so the initial-play and auto-advance flows
// share one canonical mapping.
//
// The query string carries:
//   - ?audio=N for the user's preferred-language pick (-1 = file default)
//   - ?subtitle=N for an active PGS / DVDSUB / ASS burn-in (-1 = off)
//
// Both go through the master playlist so the manager session key
// and the per-variant URLs hls.js fetches all agree on the same
// session — without this, an ABR switch would lose the burn-in (or
// the dub) silently.
async function resolvePlayerSource(itemId: string, audioStreamIndex: number, burnSubtitleIndex: number): Promise<PlayerSourceInfo> {
  const info = await api.getStreamInfo(itemId);
  const rawMethod = (info as Record<string, unknown>).method as string ?? "";
  let method: PlaybackMethod = PLAYBACK_METHOD_MAP[rawMethod] ?? "transcode";
  // Burn-in requires a transcode session (overlay needs decoded
  // frames). Force the method so a DirectPlay-eligible file still
  // routes through the master playlist when the user has picked a
  // burnable subtitle. The backend already upgrades the decision in
  // startSessionSlow; doing the same here keeps the URL pair correct.
  if (burnSubtitleIndex >= 0 && method === "direct_play") {
    method = "transcode";
  }
  const params: string[] = [];
  if (audioStreamIndex >= 0) params.push(`audio=${audioStreamIndex}`);
  if (burnSubtitleIndex >= 0) params.push(`subtitle=${burnSubtitleIndex}`);
  const query = params.length > 0 ? `?${params.join("&")}` : "";
  return {
    playbackMethod: method,
    masterPlaylistUrl:
      method !== "direct_play" ? `/api/v1/stream/${itemId}/master.m3u8${query}` : null,
    directUrl: method === "direct_play" ? `/api/v1/stream/${itemId}/direct` : null,
    audioStreamIndex,
    burnSubtitleIndex,
  };
}

// Audio-stream-index resolution lives in the playback hook because
// it depends on both the per-item media_streams list AND the user's
// global preference. Returns -1 when there's no preference, when the
// item has no audio streams matching it, or when the source isn't
// (yet) loaded.
async function resolveAudioStreamIndex(itemId: string, preferredLang: string): Promise<number> {
  if (!preferredLang) return -1;
  try {
    const item = await api.getItem(itemId);
    return pickAudioStreamIndex(item.media_streams, preferredLang);
  } catch {
    return -1;
  }
}

// Session teardown. The server's idle reaper still fires after
// IdleTimeout (~90s), but we shouldn't make every closed tab burn
// CPU + slot budget waiting for it. The api.stopStreamSession helper
// sets keepalive: true on the underlying fetch so the request
// survives page unload (chrome / firefox / safari all support that
// for short bodies, which matches our no-body DELETE exactly), and
// goes through the CSRF-aware request path so the middleware doesn't
// 403 the cleanup. When the page is alive (close player button,
// episode switch) the same call works as a normal request.
async function cleanupSession(itemId: string): Promise<void> {
  try {
    await api.stopStreamSession(itemId);
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
  /** Hooked into VideoPlayer.onAudioStreamSelected. Re-resolves the
   *  master playlist with `?audio=<streamIndex>` and primes the
   *  player to resume at `resumeAtSeconds` once the new manifest
   *  attaches. Same item id throughout — it's a swap, not an
   *  advance. */
  switchAudioStream: (streamIndex: number, resumeAtSeconds: number) => Promise<void>;
  /** Hooked into VideoPlayer.onBurnSubtitleSelected. Same shape as
   *  switchAudioStream but for PGS / DVDSUB / ASS burn-in: re-resolves
   *  the master with `?subtitle=<perTypeIndex>` (or clears it via
   *  burnSubtitleIndex=-1) and primes a resume at the current
   *  playhead so the user doesn't notice the seam. */
  switchBurnSubtitle: (subtitleIndex: number, resumeAtSeconds: number) => Promise<void>;
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
  // Reading the preference here (rather than inside resolvePlayerSource)
  // keeps the helper a pure async function — easier to memoise and
  // test — and means we re-resolve when the user changes their
  // audio language between episodes without juggling closures.
  const [preferredAudio] = useUserPreference<string>(PREFERRED_AUDIO_LANG_PREF_KEY, "");

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
        const audioIdx = await resolveAudioStreamIndex(playId, preferredAudio);
        const source = await resolvePlayerSource(playId, audioIdx, -1);

        // Resume from the saved playhead when there's progress on
        // record AND the item isn't already marked as fully watched
        // — otherwise hitting "Reproducir" on a finished episode
        // would jump straight to the credits, which feels worse
        // than starting over. We read the position off the item's
        // user_data (same shape the Continue Watching rail consumes)
        // so the resume offset matches what the rail's progress bar
        // is showing the user. Best-effort: if the lookup fails we
        // just start at zero.
        let startPosition: number | undefined;
        try {
          const it = await api.getItem(playId);
          const ticks = it.user_data?.progress?.position_ticks ?? 0;
          if (ticks > 0 && !it.user_data?.played) {
            startPosition = ticks / 10_000_000;
          }
        } catch {
          // Best-effort.
        }

        isPlayingRef.current = true;
        setPlayingItemId(playId);
        setPlayerInfo({ ...source, startPosition });
        setShowPlayer(true);
      } catch {
        setPlayError(t("itemDetail.playbackError"));
      }
    },
    [pageItemId, playingItemId, cleanup, preferredAudio, t],
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

  // Latest playerInfo is read inside the swap callbacks so an audio
  // switch preserves the active burn-in subtitle (and vice versa).
  // Using a ref instead of including playerInfo in the dependency
  // list keeps the callback identity stable — the player props
  // can't churn just because the playhead nudged.
  const playerInfoRef = useRef<PlayerSourceInfo | null>(null);
  useEffect(() => {
    playerInfoRef.current = playerInfo;
  }, [playerInfo]);

  const switchAudioStream = useCallback(
    async (streamIndex: number, resumeAtSeconds: number) => {
      if (!playingItemId) return;
      try {
        // Re-resolve the source with the new audio index. Reusing
        // resolvePlayerSource keeps the URL-shaping logic in one
        // place — we just bake in the picked index instead of
        // re-doing pickAudioStreamIndex against the user's
        // language preference. Burn-in subtitle pick (if any)
        // survives the swap.
        const currentBurnSub = playerInfoRef.current?.burnSubtitleIndex ?? -1;
        const source = await resolvePlayerSource(playingItemId, streamIndex, currentBurnSub);
        setPlayerInfo({
          ...source,
          // Threaded through to VideoPlayer's startPosition so the
          // canplay handler seeks back to the playhead the user
          // was at. The seek-reset effect on masterPlaylistUrl
          // change guarantees the gate fires again instead of
          // staying latched from the first play.
          startPosition: resumeAtSeconds,
        });
      } catch {
        // Audio switch failures are best-effort: leave the existing
        // playerInfo in place so the user keeps their current dub
        // rather than getting bumped back to the detail page.
      }
    },
    [playingItemId],
  );

  const switchBurnSubtitle = useCallback(
    async (subtitleIndex: number, resumeAtSeconds: number) => {
      if (!playingItemId) return;
      try {
        // Mirror switchAudioStream: preserve the active audio dub
        // through the swap so the user only changes the one axis
        // they touched. -1 clears the burn-in (player goes back to
        // off-subs or a native HLS sub track).
        const currentAudio = playerInfoRef.current?.audioStreamIndex ?? -1;
        const source = await resolvePlayerSource(playingItemId, currentAudio, subtitleIndex);
        setPlayerInfo({
          ...source,
          startPosition: resumeAtSeconds,
        });
      } catch {
        // Same best-effort posture as the audio swap.
      }
    },
    [playingItemId],
  );

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
        const audioIdx = await resolveAudioStreamIndex(nextEp.id, preferredAudio);
        // Auto-advance to the next episode clears any burn-in choice:
        // a PGS / ASS pick on the current episode rarely makes sense
        // for the next one (different release, different streams).
        // -1 lets the next episode start clean.
        const source = await resolvePlayerSource(nextEp.id, audioIdx, -1);
        isPlayingRef.current = true;
        setPlayerInfo(source);
      } catch {
        setShowPlayer(false);
        setPlayerInfo(null);
      }
    })();
  }, [playingItemId, siblingEpisodes, cleanup, preferredAudio]);

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
    switchAudioStream,
    switchBurnSubtitle,
  };
}
