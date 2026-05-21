import { useEffect, useMemo, useRef, useState, useCallback } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import { usePlayerStore } from "@/store/player";
import { useHls } from "@/hooks/useHls";
import { useControlsVisibility } from "@/hooks/useControlsVisibility";
import { useIsMobile } from "@/hooks/useIsMobile";
import { usePlayerKeyboard } from "@/hooks/usePlayerKeyboard";
import { useProgressReporter } from "@/hooks/useProgressReporter";
import { useTrickplay } from "@/hooks/useTrickplay";
import { PlayerControls } from "./PlayerControls";
import { UpNextOverlay, type UpNextInfo } from "./UpNextOverlay";
import { ExternalSubsModal } from "./ExternalSubsModal";
import { KeyboardHelpOverlay } from "./KeyboardHelpOverlay";
import { SkipSegmentButton } from "./SkipSegmentButton";
import { buildPickerTracksFromDB, type AudioTrack } from "./audioTracks";
import type { ExternalSubtitleResult } from "@/api/types";

// ─── Props ───────────────────────────────────────────────────────────────────

interface VideoPlayerProps {
  itemId: string;
  /**
   * When set, `itemId` is the remote item id on the named peer and
   * progress reporting routes through `/me/peers/{peerId}/items/{itemId}/progress`
   * (federation_progress) instead of the local user_data path. Local
   * playback omits this prop.
   */
  peerId?: string;
  /**
   * Federation stream session id returned by StartPeerStreamSession.
   * Set together with `peerId` for federated playback. Used to fetch
   * the federated subtitle list (master.m3u8 doesn't carry embedded
   * sub tracks across the federation boundary, so we surface them
   * via a session-scoped JSON endpoint and ride a `<track>` element
   * for the picked one — same plumbing as external subs).
   */
  peerStreamSessionId?: string;
  sessionToken: string;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
  playbackMethod: string;
  startPosition?: number;
  knownDuration?: number;
  title?: string;
  /**
   * Optional title-treatment logo URL (the same TMDb-sourced PNG the
   * hero / detail surfaces show). When present the player top-bar
   * renders it instead of the plain text title — matches what the
   * user already saw on the way into playback. Falls back to `title`
   * text when missing.
   */
  logoUrl?: string;
  /**
   * Optional backdrop image to render full-bleed BEHIND the <video>
   * element until the first frame paints. Closes the "black screen
   * for 2-5 s while ffmpeg produces the first segment" gap — same
   * Jellyfin / Plex pattern: the user sees the title artwork during
   * the prep window, fades to video on playback start. Pulled by
   * the parent from the same `item.backdrop_url` the detail page
   * already had.
   */
  backdropUrl?: string;
  /**
   * Optional next-item metadata. When provided alongside `onEnded`,
   * the player shows an "Up Next" countdown overlay when the video
   * finishes instead of triggering the callback immediately. The
   * countdown gives the user a visible chance to cancel auto-advance
   * (Plex/Netflix behaviour).
   */
  nextUp?: UpNextInfo;
  /**
   * Chapter markers to render as ticks on the seek bar. Each entry's
   * `startSeconds` is the chapter start in seconds (the parent does
   * the ticks → seconds conversion so VideoPlayer doesn't have to
   * know about the backend tick convention).
   */
  chapters?: Array<{ startSeconds: number; title: string }>;
  /**
   * Skip-intro / skip-credits / skip-recap markers from the
   * backend's segment detector. When `currentTime` falls inside a
   * range we render a floating "Saltar intro" / "Saltar créditos"
   * button bottom-right; clicking it sets `currentTime` to the
   * segment end. Confidence below 0.7 is filtered out at this
   * boundary so low-quality detector results never auto-surface
   * a button.
   */
  segments?: import("@/api/types").EpisodeSegment[];
  /**
   * Audio MediaStream rows from the DB (already filtered to
   * type === "audio"). Used to enrich the audio track picker labels
   * with codec + channel count ("English · TrueHD 7.1") instead of
   * the bare `name` hls.js exposes ("English"). Optional — without
   * it the picker falls back to the bare name.
   */
  audioStreams?: import("@/api/types").MediaStream[];
  /**
   * Per-type index of the currently-active audio stream. Drives the
   * picker's "selected" indicator. -1 = file default (whichever
   * `is_default` audio the muxer flagged); the picker resolves that
   * to the matching DB row so the user still sees a check mark.
   * Default -1 keeps existing call sites working without ceremony.
   */
  audioStreamIndex?: number;
  /**
   * Click-to-switch audio mid-playback. Player passes the picked
   * stream's per-type index + the current playhead time so the
   * parent can re-resolve the master with `?audio=N&start=<seconds>`
   * and remount us. When this prop is absent the picker falls back
   * to hls.js's setAudioTrack — which only works if the master.m3u8
   * exposes multiple in-stream tracks (atypical for HubPlay).
   */
  onAudioStreamSelected?: (streamIndex: number, currentTimeSeconds: number) => void;
  /**
   * Subtitle MediaStream rows from the DB (already filtered to
   * type === "subtitle"). Used to enumerate PGS / DVDSUB / ASS
   * tracks that the browser can't render natively; entries with
   * `IsBurnableSubtitleCodec`-matching codecs appear in the picker
   * tagged "integrado", and picking one re-mounts the player with
   * `?subtitle=N` so the backend overlays the sub into the video.
   * Optional — without it the picker only surfaces native HLS subs
   * (SRT / WebVTT).
   */
  subtitleStreams?: import("@/api/types").MediaStream[];
  /**
   * Per-type index of the subtitle currently being burned in.
   * -1 = no burn-in (the user has either no sub picked or is on a
   * native HLS sub track). Drives the picker's "selected" indicator
   * for burn-in entries so the user sees the active row checked.
   */
  burnSubtitleIndex?: number;
  /**
   * Click-to-burn subtitle mid-playback. Same shape as
   * onAudioStreamSelected — the parent rebuilds the master URL
   * with `?subtitle=N` and primes a resume at the playhead so the
   * seam between sessions is invisible. Passing -1 clears the
   * burn-in. Without this prop the burn-in entries don't appear
   * in the picker (the user can't act on them).
   */
  onBurnSubtitleSelected?: (subtitleIndex: number, currentTimeSeconds: number) => void;
  onClose: () => void;
  onEnded?: () => void;
}

// Codecs the browser can't decode natively — listed here are the ones
// we burn into the video via ffmpeg on the transcoder side. SRT/WebVTT
// ride as HLS sub tracks and are deliberately NOT included to avoid
// duplicate entries in the subtitle picker. Module-scope so the Set
// identity stays stable across renders for the burnInSubtitleEntries
// useMemo deps.
const BURNABLE_CODECS = new Set([
  "hdmv_pgs_subtitle", "pgs",
  "dvd_subtitle", "dvdsub",
  "dvb_subtitle", "dvbsub",
  "xsub",
  "ass", "ssa",
]);

// ─── Component ───────────────────────────────────────────────────────────────

const VideoPlayer: FC<VideoPlayerProps> = ({
  itemId,
  peerId,
  peerStreamSessionId,
  audioStreamIndex = -1,
  onAudioStreamSelected,
  sessionToken,
  masterPlaylistUrl,
  directUrl,
  playbackMethod,
  startPosition,
  knownDuration,
  title,
  logoUrl,
  backdropUrl,
  nextUp,
  chapters,
  segments,
  audioStreams,
  subtitleStreams,
  burnSubtitleIndex = -1,
  onBurnSubtitleSelected,
  onClose,
  onEnded: onEndedCallback,
}) => {
  const { t, i18n } = useTranslation();
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const seekedToStartRef = useRef(false);
  // Tracks the most recent reliable currentTime (timeupdate while
  // not seeking). Used by the `play` event handler to recover from
  // the "Play after pause restarts from frame 0" edge case where a
  // recoverMediaError or transient hls.js reattach zeroed out the
  // <video> element's currentTime even though the user expected it
  // to resume where they were.
  const lastGoodTimeRef = useRef(0);

  // Zustand as single source of truth for volume/mute/fullscreen
  const volume = usePlayerStore((s) => s.volume);
  const isMuted = usePlayerStore((s) => s.isMuted);
  const isFullscreen = usePlayerStore((s) => s.isFullscreen);
  const setVolume = usePlayerStore((s) => s.setVolume);
  const toggleMute = usePlayerStore((s) => s.toggleMute);
  const setFullscreen = usePlayerStore((s) => s.setFullscreen);
  const updateTime = usePlayerStore((s) => s.updateTime);

  // Local state: playback status and time (high-frequency updates, not needed globally)
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [buffered, setBuffered] = useState(0);
  // True once the first frame has painted (we listen to the
  // `playing` event, which fires when the browser actually
  // starts decoding+rendering, NOT when play() resolves). Drives
  // the backdrop loading overlay's fade-out: while false, the
  // overlay covers the (still-black) <video> with the item's
  // artwork; on flip, a CSS transition fades it out so the video
  // reveal feels cinematic instead of abrupt.
  const [firstFrameReady, setFirstFrameReady] = useState(false);
  // Up-next overlay visibility. Set on `ended` when nextUp is wired,
  // cleared by play-now / cancel / next-load. Decoupled from
  // onEndedCallback so the parent only sees the auto-advance signal
  // when the user actually consents (or the timer runs out).
  const [upNextActive, setUpNextActive] = useState(false);
  // External subs picker (OpenSubtitles, ...). Modal is opened from
  // the PlayerControls subtitle dropdown; the picked result becomes
  // a sibling `<track>` on the <video> below. Only one external sub
  // active at a time — picking a new one replaces the previous track
  // entirely.
  const [externalSubsModalOpen, setExternalSubsModalOpen] = useState(false);
  const [activeExternalSub, setActiveExternalSub] = useState<ExternalSubtitleResult | null>(null);

  // Federated subtitles. Populated once at mount when both peerId +
  // peerStreamSessionId are set; the master.m3u8 we receive from the
  // peer is variant-only (no EXT-X-MEDIA SUBTITLES), so the player
  // surfaces these through the same `<track>` plumbing the external
  // subs use. IDs above FEDERATED_TRACK_ID_BASE distinguish them
  // from HLS-native track IDs in the unified `subtitleTracks` array
  // passed to the controls dropdown.
  const [federatedSubs, setFederatedSubs] = useState<
    Array<{ index: number; language: string; title: string; default: boolean; forced: boolean }>
  >([]);
  const [activeFederatedSubIndex, setActiveFederatedSubIndex] = useState<number | null>(null);
  useEffect(() => {
    if (!peerId || !peerStreamSessionId) return;
    let cancelled = false;
    api
      .listFederatedSubtitles(peerId, peerStreamSessionId)
      .then((tracks) => {
        if (!cancelled) setFederatedSubs(tracks);
      })
      .catch(() => {
        // Silent: a failure to fetch the federated sub list shouldn't
        // break playback — the dropdown will just show the HLS tracks
        // (typically empty for federated streams) and the user keeps
        // the option of external/OpenSubtitles.
      });
    return () => {
      cancelled = true;
    };
  }, [peerId, peerStreamSessionId]);

  // ─── Hooks ─────────────────────────────────────────────────────────────────

  const {
    error,
    audioTracks,
    subtitleTracks,
    qualityLevels,
    currentAudioTrack,
    currentSubtitleTrack,
    currentQuality,
    setAudioTrack,
    setSubtitleTrack,
    setQuality,
  } = useHls({
    videoRef,
    masterPlaylistUrl,
    directUrl,
    playbackMethod,
    sessionToken,
    startPosition,
  });

  const {
    controlsVisible,
    showControls,
    hideControls,
    handleMouseMove,
    handleMouseLeave,
    keepControlsVisible,
  } = useControlsVisibility(isPlaying);

  // Mobile-aware tap pattern: on touch a tap on the video surface
  // toggles control visibility (Plex/Netflix), instead of toggling
  // play/pause. The user reaches play/pause through the (now
  // visible) play button. Desktop keeps click-to-pause because
  // mouse users expect that affordance.
  const isMobile = useIsMobile();

  // Playback rate. Persisted only for this session (refresh resets
  // to 1×). Plex / YouTube do the same — sticky preferences would
  // need a settings surface that doesn't exist yet, and 1.5× from a
  // last-watched session is jarring when revisiting.
  const [playbackRate, setPlaybackRate] = useState(1);

  useProgressReporter(videoRef, itemId, peerId);

  // Trickplay: fetched once per item. The first hit on the backend
  // triggers ffmpeg generation (5-30 s); during that window the
  // SeekBar gracefully renders without preview, then snaps in once
  // `available` flips true. No retry needed — the user is on the
  // same item for the entire session.
  const trickplay = useTrickplay(itemId);

  // ─── Sync volume/mute from store to video element ──────────────────────────

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.volume = volume;
    video.muted = isMuted;
  }, [volume, isMuted]);

  // ─── Playback controls ──────────────────────────────────────────────────

  const togglePlayPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      video.play().catch(() => {});
    } else {
      video.pause();
    }
  }, []);

  // Surface tap: on mobile this only toggles control visibility (no
  // accidental pause when the user is just trying to bring up the
  // bar). On desktop this falls through to togglePlayPause — mouse
  // users expect click-to-pause. The decision is made at click-time,
  // not via different handlers, so a viewport resize that flips
  // isMobile mid-session keeps behaviour consistent.
  const handleSurfaceTap = useCallback(() => {
    if (isMobile) {
      if (controlsVisible) {
        hideControls();
      } else {
        showControls();
      }
      return;
    }
    togglePlayPause();
  }, [isMobile, controlsVisible, hideControls, showControls, togglePlayPause]);

  // Apply playback rate to the <video> element whenever it changes.
  // Done as an effect so a remount (audio swap, recover) re-applies
  // the user's chosen rate to the new media stream automatically.
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.playbackRate = playbackRate;
  }, [playbackRate, masterPlaylistUrl, directUrl]);

  const handleSeek = useCallback((time: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.currentTime = time;
  }, []);

  const handleVolumeChange = useCallback(
    (v: number) => {
      const clamped = Math.max(0, Math.min(1, v));
      setVolume(clamped);
      if (clamped > 0 && isMuted) {
        toggleMute();
      }
    },
    [isMuted, setVolume, toggleMute],
  );

  const handleToggleMute = useCallback(() => {
    toggleMute();
  }, [toggleMute]);

  const handleToggleFullscreen = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {});
    } else {
      container.requestFullscreen().catch(() => {});
    }
  }, []);

  const handleClose = useCallback(() => {
    if (document.fullscreenElement) {
      document.exitFullscreen().then(() => onClose()).catch(() => onClose());
    } else {
      onClose();
    }
  }, [onClose]);

  // PiP toggle. Wrapped so the keyboard hook + a future toolbar
  // button share the same code path. Pre-flight failures
  // (no <video>, user-gesture missing, browser without PiP) are
  // non-fatal — silently no-op rather than throwing in the user's
  // face; the operator can still hit fullscreen as the fallback.
  const handleTogglePiP = useCallback(async () => {
    const video = videoRef.current;
    if (!video) return;
    if (!document.pictureInPictureEnabled || video.disablePictureInPicture) {
      return;
    }
    try {
      if (document.pictureInPictureElement) {
        await document.exitPictureInPicture();
      } else {
        await video.requestPictureInPicture();
      }
    } catch {
      // Ignored — pre-flight + browser-policy errors are recoverable
      // by the user trying again from a confirmed gesture.
    }
  }, []);

  // Help overlay state. `?` toggles; clicking the backdrop or
  // pressing Escape closes. The shortcut list itself is rendered
  // by KeyboardHelpOverlay below.
  const [showHelp, setShowHelp] = useState(false);
  const toggleHelp = useCallback(() => setShowHelp((v) => !v), []);

  // ─── Tab close / navigation cleanup ──────────────────────────────────────
  //
  // If the user closes the tab or navigates away (back button, address
  // bar) without pressing the player's close button, we'd otherwise
  // leak the transcode session for the server's idle timeout window
  // (~90 s). Hook into `pagehide` (more reliable than `beforeunload`,
  // also fires on iOS Safari and on bfcache eviction) and fire a
  // best-effort DELETE with `keepalive: true` so the request survives
  // unload. Going through `api.stopStreamSession` (rather than a raw
  // fetch) picks up the CSRF double-submit token the middleware
  // requires; without it the request 403'd in production. The
  // server's idle reaper is still there as a backstop if even
  // keepalive drops.
  useEffect(() => {
    const onPageHide = () => {
      void api.stopStreamSession(itemId).catch(() => {
        // Best-effort only — browser may have already torn down fetch.
      });
    };
    window.addEventListener("pagehide", onPageHide);
    return () => window.removeEventListener("pagehide", onPageHide);
  }, [itemId]);

  // ─── Keyboard shortcuts ──────────────────────────────────────────────────

  usePlayerKeyboard({
    videoRef,
    onTogglePlay: togglePlayPause,
    onToggleFullscreen: handleToggleFullscreen,
    onToggleMute: handleToggleMute,
    onVolumeChange: handleVolumeChange,
    onClose: handleClose,
    onActivity: showControls,
    onTogglePiP: () => void handleTogglePiP(),
    onToggleHelp: toggleHelp,
  });

  // ─── Seek to start position (direct_play) ────────────────────────────────

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !startPosition || seekedToStartRef.current) return;

    const onCanPlay = () => {
      if (!seekedToStartRef.current && startPosition > 0) {
        video.currentTime = startPosition;
        seekedToStartRef.current = true;
      }
    };

    video.addEventListener("canplay", onCanPlay);
    return () => video.removeEventListener("canplay", onCanPlay);
  }, [startPosition]);

  // When the source URL changes mid-session — runtime audio-track
  // switch reloads master.m3u8 with a new ?audio=N — reset the
  // seeked-to-start gate so the next canplay event re-seeks to the
  // updated startPosition. Without this, picking a different audio
  // dub while playing dropped the user back at frame 0 of the new
  // transcode (seekedToStartRef was already true from the first
  // seek and no further seek would fire).
  useEffect(() => {
    seekedToStartRef.current = false;
  }, [masterPlaylistUrl, directUrl]);

  // ─── Video event listeners ───────────────────────────────────────────────

  // Patrón "latest onEnded vía ref": el listener se monta una vez por
  // sesión de playback (deps abajo). Si añadiéramos `onEndedCallback`
  // a las deps, cada re-render del padre re-suscribiría todos los
  // listeners del <video>, perdiendo eventos durante el churn.
  const onEndedCallbackRef = useRef(onEndedCallback);
  useEffect(() => {
    onEndedCallbackRef.current = onEndedCallback;
  }, [onEndedCallback]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const onPlay = () => {
      setIsPlaying(true);
      showControls();
      // Defensive: if we somehow ended up at frame 0 even though we
      // had a remembered good position, jump back. This catches the
      // user-reported "Play after pause restarts from the beginning"
      // edge case where a recoverMediaError or detach/reattach
      // sequence zeroed out the <video> element's currentTime mid
      // session. lastGoodTimeRef is updated below in `onTimeUpdate`.
      if (video.currentTime < 1 && lastGoodTimeRef.current > 1) {
        video.currentTime = lastGoodTimeRef.current;
      }
    };

    // First-frame painted: this is what we wait for before fading
    // the backdrop overlay out. `playing` is the right event (not
    // `play`, which fires on the play() call before any frame has
    // rendered, and not `loadeddata`, which can fire before HLS
    // has wired the first segment into MSE).
    const onPlaying = () => {
      setFirstFrameReady(true);
    };

    const onPause = () => {
      setIsPlaying(false);
      keepControlsVisible();
    };

    // After a seek lands, force one resync so the React state catches
    // up immediately — without it the next `timeupdate` may be delayed
    // by hls.js wiring fresh buffer events after a transcode restart.
    const onSeeked = () => {
      setCurrentTime(video.currentTime);
    };

    const onTimeUpdate = () => {
      // Source of truth for "is a seek in flight" is the DOM property
      // `video.seeking`, NOT a React ref tracking `seeking`/`seeked`
      // events. The events can drop on the floor (most commonly when
      // the new segment never lands and hls.js gives up) and a ref
      // would then stay stuck `true` forever, freezing the seek bar
      // even though the video is actually playing again. Reading the
      // property each tick self-recovers on the next event boundary.
      if (!video.seeking) {
        setCurrentTime(video.currentTime);
        // Remember the most recent settled position so the `play`
        // handler above can recover from a zeroed-out currentTime
        // after recoverMediaError. Only update when we're past the
        // intro buffer (>0.5 s) so legitimate fresh-start sessions
        // don't accidentally save 0.
        if (video.currentTime > 0.5) {
          lastGoodTimeRef.current = video.currentTime;
        }
      }
      const videoDur = video.duration;
      const effectiveDuration =
        knownDuration && knownDuration > 0
          ? knownDuration
          : videoDur && isFinite(videoDur) && videoDur > 0
            ? videoDur
            : 0;
      setDuration(effectiveDuration);

      if (video.buffered.length > 0) {
        setBuffered(video.buffered.end(video.buffered.length - 1));
      }

      updateTime(
        video.currentTime,
        effectiveDuration,
        video.buffered.length > 0
          ? video.buffered.end(video.buffered.length - 1)
          : 0,
      );
    };

    const onEnded = () => {
      setIsPlaying(false);
      keepControlsVisible();
      api.markPlayed(itemId).catch(() => {});
      // Two paths: with a known next item, gate the auto-advance
      // behind the countdown overlay so the user can cancel; without
      // one, fire the callback immediately like the legacy flow.
      const cb = onEndedCallbackRef.current;
      if (nextUp && cb) {
        setUpNextActive(true);
      } else {
        cb?.();
      }
    };

    video.addEventListener("play", onPlay);
    video.addEventListener("playing", onPlaying);
    video.addEventListener("pause", onPause);
    video.addEventListener("seeked", onSeeked);
    video.addEventListener("timeupdate", onTimeUpdate);
    video.addEventListener("ended", onEnded);

    return () => {
      video.removeEventListener("play", onPlay);
      video.removeEventListener("playing", onPlaying);
      video.removeEventListener("pause", onPause);
      video.removeEventListener("seeked", onSeeked);
      video.removeEventListener("timeupdate", onTimeUpdate);
      video.removeEventListener("ended", onEnded);
    };
  }, [itemId, knownDuration, showControls, keepControlsVisible, updateTime, nextUp]);

  // Reset upNextActive + firstFrameReady whenever the source changes
  // — the parent's auto-advance switches `itemId`, and the new
  // episode shouldn't inherit the previous one's overlay state. The
  // canonical "key={itemId}" alternative would re-mount the whole
  // VideoPlayer and tear down the hls.js instance on every advance,
  // which is the opposite of what auto-advance is for.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    setUpNextActive(false);
    setFirstFrameReady(false);
  }, [itemId]);
  /* eslint-enable react-hooks/set-state-in-effect */

  const handleUpNextConfirm = useCallback(() => {
    setUpNextActive(false);
    onEndedCallback?.();
  }, [onEndedCallback]);

  const handleUpNextCancel = useCallback(() => {
    setUpNextActive(false);
  }, []);

  // External subs lifecycle.
  // - Opening the modal is a single setter; closing too.
  // - Picking a result: stash it as state so the JSX renders a fresh
  //   <track>. Suppress any HLS subtitle that might be active so the
  //   two systems don't race over which cues to show.
  const handleExternalSubPicked = useCallback(
    (pick: ExternalSubtitleResult) => {
      setActiveExternalSub(pick);
      setExternalSubsModalOpen(false);
      setSubtitleTrack(-1); // disable any HLS sub
      // Clear any federated track too — only one set of cues at a
      // time. Without this, both <track> elements stayed mounted and
      // the two rAF effects raced for which one ended in `showing`.
      setActiveFederatedSubIndex(null);
    },
    [setSubtitleTrack],
  );

  // After a new external <track> mounts the browser keeps its mode
  // at "disabled" by default. We force it to "showing" once it's
  // actually in the DOM. Keying on the active sub identity guarantees
  // the effect re-runs when the user picks a different one.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !activeExternalSub) return;
    // The DOM may not have applied the new <track> on the first
    // microtask; wait one rAF before flipping the mode.
    const rafID = window.requestAnimationFrame(() => {
      const tracks = Array.from(video.textTracks);
      // The external track is the one whose label starts with
      // "External:" — set inside the JSX below.
      const target = tracks.find((t) => t.label.startsWith("External:"));
      if (target) target.mode = "showing";
      // Suppress any other text tracks the user didn't ask for so we
      // don't end up double-rendering cues from an HLS sub.
      for (const t of tracks) {
        if (t !== target && t.mode === "showing") {
          t.mode = "disabled";
        }
      }
    });
    return () => window.cancelAnimationFrame(rafID);
  }, [activeExternalSub]);

  // Force the federated `<track>` into `showing` once mounted —
  // identical reasoning to the external-subs effect above. Keying on
  // the active index re-runs the effect when the user picks a
  // different federated track.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || activeFederatedSubIndex === null) return;
    const rafID = window.requestAnimationFrame(() => {
      const tracks = Array.from(video.textTracks);
      const target = tracks.find((t) => t.label.startsWith("Federated:"));
      if (target) target.mode = "showing";
      for (const t of tracks) {
        if (t !== target && t.mode === "showing") {
          t.mode = "disabled";
        }
      }
    });
    return () => window.cancelAnimationFrame(rafID);
  }, [activeFederatedSubIndex]);

  // Merge federated tracks into the dropdown's track list. IDs from
  // FEDERATED_TRACK_ID_BASE up are reserved for federated subs so the
  // routing logic in handleSubtitleTrackChange can tell them apart
  // from hls.js-native track ids (which are 0..N-1).
  const FEDERATED_TRACK_ID_BASE = 10000;
  // Burn-in subtitle id space sits ABOVE federation (20000+) so the
  // dispatch in handleSubtitleTrackChange stays a single if-ladder.
  // The id is BURN_SUB_TRACK_ID_BASE + perTypeSubtitleIndex so the
  // parent's onBurnSubtitleSelected receives the index the manager
  // expects directly, no extra lookup table.
  const BURN_SUB_TRACK_ID_BASE = 20000;

  // Build burn-in subtitle picker entries from the item's
  // MediaStream rows. Each row gets a stable per-type index (the
  // 0-based position among subtitle streams) so the URL param
  // ?subtitle=N matches the index ffmpeg's 0:s:N reference uses.
  // BURNABLE_CODECS lives at module scope (top of file) so the Set
  // identity is stable and the useMemo deps stay correct.
  const burnInSubtitleEntries = useMemo(() => {
    if (!subtitleStreams || !onBurnSubtitleSelected) return [];
    const out: { id: number; name: string; lang: string; burnIn: true }[] = [];
    let subOrd = -1;
    for (const s of subtitleStreams) {
      if (s.type !== "subtitle") continue;
      subOrd++;
      if (!BURNABLE_CODECS.has((s.codec || "").toLowerCase())) continue;
      out.push({
        id: BURN_SUB_TRACK_ID_BASE + subOrd,
        name: s.title || s.language || `Track ${subOrd + 1}`,
        lang: s.language || "",
        burnIn: true,
      });
    }
    return out;
  }, [subtitleStreams, onBurnSubtitleSelected]);

  const showFederatedTracks = !!peerId && !!peerStreamSessionId && federatedSubs.length > 0;
  const mergedSubtitleTracks = [
    ...subtitleTracks,
    ...(showFederatedTracks
      ? federatedSubs.map((s, i) => ({
          id: FEDERATED_TRACK_ID_BASE + i,
          name: s.title || s.language || `Track ${s.index}`,
          lang: s.language || "",
        }))
      : []),
    ...burnInSubtitleEntries,
  ];
  const effectiveCurrentSubtitleTrack =
    activeFederatedSubIndex !== null
      ? FEDERATED_TRACK_ID_BASE + activeFederatedSubIndex
      : burnSubtitleIndex >= 0
        ? BURN_SUB_TRACK_ID_BASE + burnSubtitleIndex
        : currentSubtitleTrack;

  const handleSubtitleTrackChange = useCallback(
    (id: number) => {
      if (id >= BURN_SUB_TRACK_ID_BASE) {
        // Burn-in pick: clear every other sub surface so only the
        // burned-in stream is shown after the remount completes,
        // then ask the parent to re-resolve the master with the
        // new ?subtitle=N param. The current playhead is captured
        // so the new manifest seeks back to where the user was.
        if (!onBurnSubtitleSelected) return;
        setActiveFederatedSubIndex(null);
        setActiveExternalSub(null);
        setSubtitleTrack(-1);
        const subIdx = id - BURN_SUB_TRACK_ID_BASE;
        onBurnSubtitleSelected(subIdx, videoRef.current?.currentTime ?? 0);
        return;
      }
      if (id >= FEDERATED_TRACK_ID_BASE) {
        // Pick a federated track. Suppress HLS subs + external subs
        // so only one set of cues renders at a time.
        setActiveFederatedSubIndex(id - FEDERATED_TRACK_ID_BASE);
        setActiveExternalSub(null);
        setSubtitleTrack(-1);
        return;
      }
      // HLS path (or "off" with id=-1). Clear federated sub state so
      // its `<track>` element unmounts. Also clear the burn-in if
      // one is active — the user is explicitly switching to no-sub
      // or to a native HLS track, neither of which co-exists with
      // burn-in. -1 triggers the manager to spin down the transcode
      // session and start a fresh non-burn one on next play.
      setActiveFederatedSubIndex(null);
      if (burnSubtitleIndex >= 0 && onBurnSubtitleSelected) {
        onBurnSubtitleSelected(-1, videoRef.current?.currentTime ?? 0);
      }
      setSubtitleTrack(id);
    },
    [setSubtitleTrack, onBurnSubtitleSelected, burnSubtitleIndex],
  );

  // Audio picker entries. When the parent provides DB MediaStream
  // rows AND a switch callback, build the picker straight from the
  // file's audio inventory with rich labels ("Castellano · DD+ 5.1
  // · Predeterminado") that mirror what Plex / Jellyfin show. This
  // path is the normal HubPlay route — the master.m3u8 transcodes a
  // single track per session, so hls.js's audio-track list only
  // sees that one entry; without the DB-driven picker the user
  // can't see the other languages exist. Falls back to the bare
  // hls.js list when the callback isn't wired (legacy callers /
  // sessions without DB metadata).
  const audioLocale: "es" | "en" = i18n.language?.startsWith("en") ? "en" : "es";
  const dbDrivenAudioTracks = useMemo<AudioTrack[]>(() => {
    if (!audioStreams || !onAudioStreamSelected) return [];
    return buildPickerTracksFromDB(
      audioStreams,
      audioLocale,
      audioLocale === "es" ? "Predeterminado" : "Default",
    );
  }, [audioStreams, onAudioStreamSelected, audioLocale]);

  const useDbAudioPicker = dbDrivenAudioTracks.length > 1;

  // Resolve which entry shows the check. -1 from the parent means
  // "use the file's default audio" — we map that to the row whose
  // is_default flag is set so the picker still shows a checkmark
  // (matches Jellyfin's UX). Falls back to the first row if no
  // stream is flagged default.
  const defaultStreamPerTypeIndex = useMemo<number>(() => {
    if (!audioStreams) return 0;
    let idx = -1;
    let firstAudio = -1;
    for (const s of audioStreams) {
      if (s.type !== "audio") continue;
      idx++;
      if (firstAudio === -1) firstAudio = idx;
      if (s.is_default) return idx;
    }
    return firstAudio === -1 ? 0 : firstAudio;
  }, [audioStreams]);

  const displayAudioTracks = useDbAudioPicker ? dbDrivenAudioTracks : audioTracks;
  const displayCurrentAudioTrack = useDbAudioPicker
    ? (audioStreamIndex >= 0 ? audioStreamIndex : defaultStreamPerTypeIndex)
    : currentAudioTrack;
  const handleAudioTrackChange = useCallback(
    (id: number) => {
      if (useDbAudioPicker && onAudioStreamSelected) {
        // Capture the playhead so the parent can resume at the
        // same spot after the master reloads with the new ?audio=N.
        // currentTime is already the live value from the <video>
        // element, no need to round-trip through state.
        const at = videoRef.current?.currentTime ?? 0;
        onAudioStreamSelected(id, at);
        return;
      }
      setAudioTrack(id);
    },
    [useDbAudioPicker, onAudioStreamSelected, setAudioTrack],
  );

  // ─── Fullscreen change listener ──────────────────────────────────────────

  useEffect(() => {
    const onFullscreenChange = () => {
      setFullscreen(!!document.fullscreenElement);
    };

    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () =>
      document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, [setFullscreen]);

  // ─── Render ──────────────────────────────────────────────────────────────

  return (
    // role="application" indica al lector de pantalla que es un widget
    // interactivo (el player). El onClick gestiona tap-to-pause /
    // tap-to-show-controls; los atajos de teclado los maneja
    // usePlayerKeyboard a nivel window.
    <div
      ref={containerRef}
      role="application"
      aria-label={t("player.label", { defaultValue: "Reproductor de video" })}
      className="fixed inset-0 z-50 bg-black select-none"
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
      onClick={handleSurfaceTap}
      onKeyDown={(e) => {
        // Tap/Space sobre la superficie despierta los controles
        // (espejo del onClick). El resto de atajos los maneja
        // usePlayerKeyboard a nivel window.
        if (e.key === " " || e.key === "Enter") handleSurfaceTap();
      }}
    >
      {/* Video element. External subtitles ride as a child <track>:
          the browser decodes the WebVTT and renders cues natively, so
          we don't have to plumb anything through hls.js. The label
          prefix ("External:") is the discriminator used by the
          textTracks effect above to enable the right one when a new
          pick lands. crossOrigin is left unset because the endpoint
          is same-origin (cookie auth flows automatically). */}
      <video
        ref={videoRef}
        className="absolute inset-0 size-full object-contain"
        playsInline
        // We DO want Picture-in-Picture (the `p` shortcut +
        // future toolbar button rely on it), so disablePictureInPicture
        // is intentionally absent. We still suppress the download +
        // remote-playback hints that appear on long-press / right-click
        // on some platforms; PiP is opt-in via our own UI, not the
        // browser chrome.
        controlsList="nodownload noremoteplayback"
        onClick={(e) => e.stopPropagation()}
        onDoubleClick={(e) => {
          e.stopPropagation();
          handleToggleFullscreen();
        }}
      >
        {activeExternalSub && (
          <track
            key={`${activeExternalSub.source}:${activeExternalSub.file_id}`}
            kind="subtitles"
            srcLang={activeExternalSub.language}
            label={`External:${activeExternalSub.language}`}
            src={api.externalSubtitleURL(itemId, activeExternalSub.source, activeExternalSub.file_id)}
          />
        )}
        {peerId && peerStreamSessionId && activeFederatedSubIndex !== null
          && federatedSubs[activeFederatedSubIndex] && (
            <track
              key={`fed:${federatedSubs[activeFederatedSubIndex].index}`}
              kind="subtitles"
              srcLang={federatedSubs[activeFederatedSubIndex].language}
              label={`Federated:${federatedSubs[activeFederatedSubIndex].language || activeFederatedSubIndex}`}
              src={api.federatedSubtitleURL(peerId, peerStreamSessionId, federatedSubs[activeFederatedSubIndex].index)}
            />
        )}
      </video>

      {/* Backdrop loading overlay. Sits ABOVE the <video> until the
          first frame paints (`playing` event), then fades out via the
          opacity transition. Renders the item's backdrop full-bleed
          with a soft dark gradient so the title/logo stays legible
          on bright artwork (Avengers / Doctor Strange / etc. have
          near-white skies). `pointer-events-none` once faded so it
          never intercepts clicks. Subtle pulsing thin bar at top
          telegraphs "preparing" without a Windows-95 spinner. */}
      <div
        className={[
          "absolute inset-0 transition-opacity duration-500 ease-out",
          firstFrameReady ? "opacity-0 pointer-events-none" : "opacity-100",
        ].join(" ")}
        aria-hidden={firstFrameReady}
      >
        {/* Top progress bar (thin, indeterminate). 0.5 px tall so the
            artwork dominates; the slide animation travels a 25%-wide
            highlight left → right via transform-only animation so it
            composites on the GPU and doesn't perturb the rest of the
            overlay. */}
        <div className="absolute top-0 left-0 right-0 h-0.5 bg-white/10 overflow-hidden">
          <div
            className="h-full w-1/4 bg-white/70"
            style={{ animation: "loading-slide 900ms ease-in-out infinite" }}
          />
        </div>

        {/* Backdrop image, scaled to cover. Falls back to a flat
            black surface when no backdrop was passed (federated
            items, scan-in-progress items, etc.) — still better than
            the bare-black <video> because the title/logo is on
            screen. */}
        <div
          className="absolute inset-0 bg-black"
          style={
            backdropUrl
              ? {
                  backgroundImage: `url(${backdropUrl})`,
                  backgroundSize: "cover",
                  backgroundPosition: "center",
                }
              : undefined
          }
        />
        {/* Dark gradient: ensures the title/logo at the bottom-left
            reads cleanly regardless of the underlying frame. Plex /
            Jellyfin both rely on this same vignette pattern. */}
        <div className="absolute inset-0 bg-gradient-to-t from-black via-black/40 to-black/30" />

        {/* Title treatment in the bottom-left, mirroring the hero
            page the user came from. Logo wins when present (matches
            Plex / Apple TV); plain text falls back so we never end
            up with an empty corner. */}
        <div className="absolute left-6 right-6 bottom-12 sm:left-12 sm:bottom-20 max-w-[60%]">
          {logoUrl ? (
            <img
              src={logoUrl}
              alt={title ?? ""}
              className="max-h-24 sm:max-h-32 w-auto object-contain drop-shadow-[0_4px_12px_rgba(0,0,0,0.7)]"
            />
          ) : title ? (
            <h1 className="text-3xl sm:text-5xl font-semibold text-white drop-shadow-[0_4px_12px_rgba(0,0,0,0.8)]">
              {title}
            </h1>
          ) : null}
        </div>
      </div>

      {/* Error overlay */}
      {error && (
        <div className="absolute inset-0 flex items-center justify-center z-30 bg-black/80">
          <div className="flex flex-col items-center gap-4 max-w-md px-6 text-center">
            <svg
              className="size-12 text-error"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z" />
            </svg>
            <p className="text-sm text-text-secondary">{error}</p>
            <button
              onClick={(e) => {
                e.stopPropagation();
                handleClose();
              }}
              className="px-4 py-2 bg-white/10 hover:bg-white/20 rounded-[--radius-md] text-sm text-white transition-colors cursor-pointer"
            >
              {t("playerControls.closePlayer")}
            </button>
          </div>
        </div>
      )}

      {/* Capa de controles. Sólo intercepta clicks/teclas para que no
          burbujeen al video (que dispararía play/pause). role="toolbar"
          comunica que contiene controles agrupados al lector de pantalla. */}
      <div
        role="toolbar"
        aria-label={t("player.controlsLabel", { defaultValue: "Controles del reproductor" })}
        className={[
          "absolute inset-0 transition-opacity duration-300",
          controlsVisible ? "opacity-100" : "opacity-0 pointer-events-none",
        ].join(" ")}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <PlayerControls
          isPlaying={isPlaying}
          currentTime={currentTime}
          duration={duration}
          buffered={buffered}
          volume={volume}
          isMuted={isMuted}
          isFullscreen={isFullscreen}
          audioTracks={displayAudioTracks}
          audioStreams={useDbAudioPicker ? undefined : audioStreams}
          subtitleTracks={mergedSubtitleTracks}
          qualityLevels={qualityLevels}
          chapters={chapters}
          currentAudioTrack={displayCurrentAudioTrack}
          currentSubtitleTrack={effectiveCurrentSubtitleTrack}
          currentQuality={currentQuality}
          playbackRate={playbackRate}
          onPlayPause={togglePlayPause}
          onSeek={handleSeek}
          onVolumeChange={handleVolumeChange}
          onToggleMute={handleToggleMute}
          onToggleFullscreen={handleToggleFullscreen}
          onAudioTrackChange={handleAudioTrackChange}
          onSubtitleTrackChange={handleSubtitleTrackChange}
          onQualityChange={setQuality}
          onPlaybackRateChange={setPlaybackRate}
          onMenuOpenChange={(open) => {
            // While a picker is up, pin controls visible so the 3s
            // auto-hide timer can't evict the overlay (and the sheet
            // hanging off it) mid-interaction. On close, restart
            // the timer via showControls() so the bar can fade
            // again once the user is back on the video.
            if (open) keepControlsVisible();
            else showControls();
          }}
          onSearchExternalSubs={() => setExternalSubsModalOpen(true)}
          trickplay={trickplay.available && trickplay.manifest ? {
            manifest: trickplay.manifest,
            spriteURL: trickplay.spriteURL,
          } : undefined}
          onClose={handleClose}
          title={title}
          logoUrl={logoUrl}
          playbackMethod={
            playbackMethod === "direct_play" || playbackMethod === "direct_stream" || playbackMethod === "transcode"
              ? playbackMethod
              : undefined
          }
          // Show the active variant only for transcoded sessions —
          // direct_play / direct_stream serve the source as-is so a
          // "1080p" badge there would lie. The current quality
          // index maps back to the variant label hls.js exposed.
          transcodeProfileLabel={
            playbackMethod === "transcode" && currentQuality !== undefined
              ? qualityLevels.find((q) => q.id === currentQuality)?.label
              : undefined
          }
        />
      </div>

      {/* External subs picker. Mounted at the player root so it
          covers controls but its own click-to-close (via backdrop)
          doesn't accidentally pause the video. The picked result
          flows into a sibling <track> on the <video> via state. */}
      {externalSubsModalOpen && (
        <ExternalSubsModal
          itemId={itemId}
          onSelect={handleExternalSubPicked}
          onClose={() => setExternalSubsModalOpen(false)}
        />
      )}

      {/* Skip-intro / skip-credits / skip-recap floating button.
          Sits ABOVE the controls (z-30) but below the up-next
          overlay (z-40) so an active up-next prompt can't be
          partially hidden by a stale skip button. The button is
          suppressed entirely while up-next is active because
          control of attention belongs to that prompt at end-of-
          episode time. */}
      {!upNextActive && (
        <SkipSegmentButton
          segments={segments}
          currentTime={currentTime}
          onSkip={handleSeek}
          nextUpAvailable={!!nextUp}
        />
      )}

      {/* Up-next overlay — sits above the controls when active so the
          user's first focus target is the auto-advance prompt rather
          than the (now-stuck) play button. */}
      {upNextActive && nextUp && (
        // role="presentation": el wrapper sólo evita que clicks/teclas
        // burbujeen al video (que dispararía play/pause); el botón
        // interno del overlay es el elemento interactivo real.
        <div
          role="presentation"
          className="absolute inset-0 z-40 flex items-end justify-end p-6 sm:p-10 bg-gradient-to-t from-black/70 via-black/30 to-transparent"
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <UpNextOverlay
            nextUp={nextUp}
            onPlayNow={handleUpNextConfirm}
            onCancel={handleUpNextCancel}
          />
        </div>
      )}

      {/* Keyboard shortcuts overlay (toggled with `?`). z-50 so it
          floats above controls + up-next; backdrop click closes
          since the operator's expectation is "anywhere outside the
          card dismisses". */}
      {showHelp && (
        <KeyboardHelpOverlay onClose={() => setShowHelp(false)} />
      )}
    </div>
  );
};

export { VideoPlayer };
