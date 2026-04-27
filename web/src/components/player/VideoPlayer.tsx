import { useEffect, useRef, useState, useCallback } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import { usePlayerStore } from "@/store/player";
import { useHls } from "@/hooks/useHls";
import { useControlsVisibility } from "@/hooks/useControlsVisibility";
import { usePlayerKeyboard } from "@/hooks/usePlayerKeyboard";
import { useProgressReporter } from "@/hooks/useProgressReporter";
import { PlayerControls } from "./PlayerControls";
import { UpNextOverlay, type UpNextInfo } from "./UpNextOverlay";
import { ExternalSubsModal } from "./ExternalSubsModal";
import type { ExternalSubtitleResult } from "@/api/types";

// ─── Props ───────────────────────────────────────────────────────────────────

interface VideoPlayerProps {
  itemId: string;
  sessionToken: string;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
  playbackMethod: string;
  startPosition?: number;
  knownDuration?: number;
  title?: string;
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
  onClose: () => void;
  onEnded?: () => void;
}

// ─── Component ───────────────────────────────────────────────────────────────

const VideoPlayer: FC<VideoPlayerProps> = ({
  itemId,
  sessionToken,
  masterPlaylistUrl,
  directUrl,
  playbackMethod,
  startPosition,
  knownDuration,
  title,
  nextUp,
  chapters,
  onClose,
  onEnded: onEndedCallback,
}) => {
  const { t } = useTranslation();
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const seekedToStartRef = useRef(false);

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

  useProgressReporter(videoRef, itemId);

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
    };

    const onPause = () => {
      setIsPlaying(false);
      keepControlsVisible();
    };

    const onTimeUpdate = () => {
      setCurrentTime(video.currentTime);
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
    video.addEventListener("pause", onPause);
    video.addEventListener("timeupdate", onTimeUpdate);
    video.addEventListener("ended", onEnded);

    return () => {
      video.removeEventListener("play", onPlay);
      video.removeEventListener("pause", onPause);
      video.removeEventListener("timeupdate", onTimeUpdate);
      video.removeEventListener("ended", onEnded);
    };
  }, [itemId, knownDuration, showControls, keepControlsVisible, updateTime, onEndedCallback, nextUp]);

  // Reset upNextActive whenever the source changes — the parent's
  // auto-advance switches `itemId`, and the new episode shouldn't
  // inherit the previous one's overlay state.
  useEffect(() => {
    setUpNextActive(false);
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
          onClose={handleClose}
          title={title}
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
