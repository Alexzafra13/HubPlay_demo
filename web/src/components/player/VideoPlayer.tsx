import { useEffect, useRef, useState, useCallback } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import { usePlayerStore } from "@/store/player";
import { useHls } from "@/hooks/useHls";
import { useControlsVisibility } from "@/hooks/useControlsVisibility";
import { usePlayerKeyboard } from "@/hooks/usePlayerKeyboard";
import { useProgressReporter } from "@/hooks/useProgressReporter";
import { useTrickplay } from "@/hooks/useTrickplay";
import { PlayerControls } from "./PlayerControls";
import { UpNextOverlay, type UpNextInfo } from "./UpNextOverlay";
import { ExternalSubsModal } from "./ExternalSubsModal";
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
   * Audio MediaStream rows from the DB (already filtered to
   * type === "audio"). Used to enrich the audio track picker labels
   * with codec + channel count ("English · TrueHD 7.1") instead of
   * the bare `name` hls.js exposes ("English"). Optional — without
   * it the picker falls back to the bare name.
   */
  audioStreams?: import("@/api/types").MediaStream[];
  onClose: () => void;
  onEnded?: () => void;
}

// ─── Component ───────────────────────────────────────────────────────────────

const VideoPlayer: FC<VideoPlayerProps> = ({
  itemId,
  peerId,
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
  audioStreams,
  onClose,
  onEnded: onEndedCallback,
}) => {
  const { t } = useTranslation();
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
    handleMouseMove,
    handleMouseLeave,
    keepControlsVisible,
  } = useControlsVisibility(isPlaying);

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

  // ─── Tab close / navigation cleanup ──────────────────────────────────────
  //
  // If the user closes the tab or navigates away (back button, address
  // bar) without pressing the player's close button, we'd otherwise
  // leak the transcode session for the server's idle timeout window
  // (~90 s). Hook into `pagehide` (more reliable than `beforeunload`,
  // also fires on iOS Safari and on bfcache eviction) and fire a
  // best-effort DELETE with `keepalive: true` from the cleanup helper
  // so the request survives unload. The server's idle reaper is still
  // there as a backstop if even this drops.
  useEffect(() => {
    const onPageHide = () => {
      const token = localStorage.getItem("hubplay_access_token");
      try {
        fetch(`/api/v1/stream/${itemId}/session`, {
          method: "DELETE",
          headers: token ? { Authorization: `Bearer ${token}` } : {},
          keepalive: true,
        }).catch(() => {});
      } catch {
        // Browser may have already torn down fetch — best-effort only.
      }
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

  // ─── Video event listeners ───────────────────────────────────────────────

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
      if (nextUp && onEndedCallback) {
        setUpNextActive(true);
      } else {
        onEndedCallback?.();
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
  }, [itemId, knownDuration, showControls, keepControlsVisible, updateTime, onEndedCallback, nextUp]);

  // Reset upNextActive whenever the source changes — the parent's
  // auto-advance switches `itemId`, and the new episode shouldn't
  // inherit the previous one's overlay state.
  useEffect(() => {
    setUpNextActive(false);
    // Same rationale for firstFrameReady: a next-up advance reuses
    // the same VideoPlayer instance with a new source. Without the
    // reset the loading overlay would NOT show during the prep
    // window of the next episode (it'd think the first frame had
    // already painted from the previous one).
    setFirstFrameReady(false);
  }, [itemId]);

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
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 bg-black select-none"
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
      onClick={togglePlayPause}
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
        className="absolute inset-0 w-full h-full object-contain"
        playsInline
        // Suppress Chrome's floating PiP toggle that hovers near the
        // bottom-right of the video element (intrudes on our own
        // controls overlay) plus the download / remote-playback hints
        // that appear on long-press / right-click on some platforms.
        disablePictureInPicture
        controlsList="nodownload nopictureinpicture noremoteplayback"
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
            style={{ animation: "loading-slide 1.8s ease-in-out infinite" }}
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
            <h1 className="text-3xl sm:text-5xl font-bold text-white drop-shadow-[0_4px_12px_rgba(0,0,0,0.8)]">
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
              className="h-12 w-12 text-error"
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

      {/* Controls overlay */}
      <div
        className={[
          "absolute inset-0 transition-opacity duration-300",
          controlsVisible ? "opacity-100" : "opacity-0 pointer-events-none",
        ].join(" ")}
        onClick={(e) => e.stopPropagation()}
      >
        <PlayerControls
          isPlaying={isPlaying}
          currentTime={currentTime}
          duration={duration}
          buffered={buffered}
          volume={volume}
          isMuted={isMuted}
          isFullscreen={isFullscreen}
          audioTracks={audioTracks}
          audioStreams={audioStreams}
          subtitleTracks={subtitleTracks}
          qualityLevels={qualityLevels}
          chapters={chapters}
          currentAudioTrack={currentAudioTrack}
          currentSubtitleTrack={currentSubtitleTrack}
          currentQuality={currentQuality}
          onPlayPause={togglePlayPause}
          onSeek={handleSeek}
          onVolumeChange={handleVolumeChange}
          onToggleMute={handleToggleMute}
          onToggleFullscreen={handleToggleFullscreen}
          onAudioTrackChange={setAudioTrack}
          onSubtitleTrackChange={setSubtitleTrack}
          onQualityChange={setQuality}
          onSearchExternalSubs={() => setExternalSubsModalOpen(true)}
          trickplay={trickplay.available && trickplay.manifest ? {
            manifest: trickplay.manifest,
            spriteURL: trickplay.spriteURL,
          } : undefined}
          onClose={handleClose}
          title={title}
          logoUrl={logoUrl}
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

      {/* Up-next overlay — sits above the controls when active so the
          user's first focus target is the auto-advance prompt rather
          than the (now-stuck) play button. */}
      {upNextActive && nextUp && (
        <div
          className="absolute inset-0 z-40 flex items-end justify-end p-6 sm:p-10 bg-gradient-to-t from-black/70 via-black/30 to-transparent"
          onClick={(e) => e.stopPropagation()}
        >
          <UpNextOverlay
            nextUp={nextUp}
            onPlayNow={handleUpNextConfirm}
            onCancel={handleUpNextCancel}
          />
        </div>
      )}
    </div>
  );
};

export { VideoPlayer };
export type { VideoPlayerProps };
