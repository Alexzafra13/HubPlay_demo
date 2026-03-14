import { useEffect, useRef, useState, useCallback } from "react";
import type { FC } from "react";
import Hls from "hls.js";
import { api } from "@/api/client";
import { usePlayerStore } from "@/store/player";
import { PlayerControls } from "./PlayerControls";
import type { AudioTrack, SubtitleTrack } from "./PlayerControls";

// ─── Props ───────────────────────────────────────────────────────────────────

interface VideoPlayerProps {
  itemId: string;
  sessionToken: string;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
  playbackMethod: string;
  startPosition?: number;
  title?: string;
  onClose: () => void;
}

// ─── Constants ───────────────────────────────────────────────────────────────

const CONTROLS_HIDE_DELAY = 3000;
const PROGRESS_SAVE_INTERVAL = 10_000;
const TICKS_PER_SECOND = 10_000_000;

// ─── Component ───────────────────────────────────────────────────────────────

const VideoPlayer: FC<VideoPlayerProps> = ({
  itemId,
  sessionToken,
  masterPlaylistUrl,
  directUrl,
  playbackMethod,
  startPosition,
  title,
  onClose,
}) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout>>(0 as never);
  const progressTimerRef = useRef<ReturnType<typeof setInterval>>(0 as never);
  const seekedToStartRef = useRef(false);

  // Player state
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [buffered, setBuffered] = useState(0);
  const [volume, setVolumeState] = useState(() => usePlayerStore.getState().volume);
  const [isMuted, setIsMuted] = useState(() => usePlayerStore.getState().isMuted);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [controlsVisible, setControlsVisible] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Track state
  const [audioTracks, setAudioTracks] = useState<AudioTrack[]>([]);
  const [subtitleTracks, setSubtitleTracks] = useState<SubtitleTrack[]>([]);
  const [currentAudioTrack, setCurrentAudioTrack] = useState(0);
  const [currentSubtitleTrack, setCurrentSubtitleTrack] = useState(-1);

  const store = usePlayerStore;

  // ─── Controls visibility ────────────────────────────────────────────────

  const showControls = useCallback(() => {
    setControlsVisible(true);
    clearTimeout(hideTimerRef.current);
    hideTimerRef.current = setTimeout(() => {
      if (videoRef.current && !videoRef.current.paused) {
        setControlsVisible(false);
      }
    }, CONTROLS_HIDE_DELAY);
  }, []);

  const handleMouseMove = useCallback(() => {
    showControls();
  }, [showControls]);

  const handleMouseLeave = useCallback(() => {
    if (videoRef.current && !videoRef.current.paused) {
      clearTimeout(hideTimerRef.current);
      hideTimerRef.current = setTimeout(() => {
        setControlsVisible(false);
      }, 800);
    }
  }, []);

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

  const handleVolumeChange = useCallback((v: number) => {
    const video = videoRef.current;
    if (!video) return;
    const clamped = Math.max(0, Math.min(1, v));
    video.volume = clamped;
    setVolumeState(clamped);
    store.getState().setVolume(clamped);
    if (clamped > 0 && isMuted) {
      video.muted = false;
      setIsMuted(false);
      store.getState().toggleMute();
    }
  }, [isMuted, store]);

  const handleToggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    video.muted = !video.muted;
    setIsMuted(video.muted);
    store.getState().toggleMute();
  }, [store]);

  const handleToggleFullscreen = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {});
    } else {
      container.requestFullscreen().catch(() => {});
    }
  }, []);

  const handleAudioTrackChange = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.audioTrack = id;
      setCurrentAudioTrack(id);
    }
  }, []);

  const handleSubtitleTrackChange = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.subtitleTrack = id;
      setCurrentSubtitleTrack(id);
    }
  }, []);

  // ─── Close handler (exit fullscreen first if needed) ─────────────────────

  const handleClose = useCallback(() => {
    if (document.fullscreenElement) {
      document.exitFullscreen().then(() => onClose()).catch(() => onClose());
    } else {
      onClose();
    }
  }, [onClose]);

  // ─── Initialize video source ─────────────────────────────────────────────

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    // Apply stored volume
    video.volume = volume;
    video.muted = isMuted;

    const useHls = playbackMethod === "transcode" || playbackMethod === "direct_stream";

    if (useHls && masterPlaylistUrl) {
      const url = `${masterPlaylistUrl}${masterPlaylistUrl.includes("?") ? "&" : "?"}token=${sessionToken}`;

      if (Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: false,
          startPosition: startPosition ?? -1,
          xhrSetup: (xhr) => {
            xhr.withCredentials = false;
          },
        });

        hlsRef.current = hls;

        hls.loadSource(url);
        hls.attachMedia(video);

        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          // Populate audio tracks
          const aTracks: AudioTrack[] = hls.audioTracks.map((t) => ({
            id: t.id,
            name: t.name,
            lang: t.lang || "",
          }));
          setAudioTracks(aTracks);
          setCurrentAudioTrack(hls.audioTrack);

          video.play().catch(() => {});
        });

        hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
          const sTracks: SubtitleTrack[] = hls.subtitleTracks.map((t) => ({
            id: t.id,
            name: t.name,
            lang: t.lang || "",
          }));
          setSubtitleTracks(sTracks);
          setCurrentSubtitleTrack(hls.subtitleTrack);
        });

        hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, (_event, data) => {
          setCurrentAudioTrack(data.id);
        });

        hls.on(Hls.Events.SUBTITLE_TRACK_SWITCH, (_event, data) => {
          setCurrentSubtitleTrack(data.id);
        });

        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) {
            switch (data.type) {
              case Hls.ErrorTypes.NETWORK_ERROR:
                setError("A network error occurred. Attempting to recover...");
                hls.startLoad();
                break;
              case Hls.ErrorTypes.MEDIA_ERROR:
                setError("A media error occurred. Attempting to recover...");
                hls.recoverMediaError();
                break;
              default:
                setError(`Playback failed: ${data.details}`);
                hls.destroy();
                break;
            }
          }
        });
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        // Native HLS (Safari)
        video.src = url;
        video.addEventListener("loadedmetadata", () => {
          video.play().catch(() => {});
        }, { once: true });
      } else {
        setError("HLS playback is not supported in this browser.");
      }
    } else if (playbackMethod === "direct_play" && directUrl) {
      video.src = directUrl;
      video.addEventListener("loadedmetadata", () => {
        video.play().catch(() => {});
      }, { once: true });
    } else {
      setError("No playback source available.");
    }

    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
    };
    // Only run on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ─── Seek to start position after canplay (for direct_play) ──────────────

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
      setIsPlaying(true);
      // Keep controls visible when paused
      setIsPlaying(false);
      setControlsVisible(true);
      clearTimeout(hideTimerRef.current);
    };

    const onTimeUpdate = () => {
      setCurrentTime(video.currentTime);
      setDuration(video.duration || 0);

      // Buffered
      if (video.buffered.length > 0) {
        setBuffered(video.buffered.end(video.buffered.length - 1));
      }

      // Sync to store
      store.getState().updateTime(
        video.currentTime,
        video.duration || 0,
        video.buffered.length > 0 ? video.buffered.end(video.buffered.length - 1) : 0,
      );
    };

    const onEnded = () => {
      setIsPlaying(false);
      setControlsVisible(true);
      api.markPlayed(itemId).catch(() => {});
    };

    const onError = () => {
      if (video.error) {
        setError(`Playback error: ${video.error.message || "Unknown error"}`);
      }
    };

    video.addEventListener("play", onPlay);
    video.addEventListener("pause", onPause);
    video.addEventListener("timeupdate", onTimeUpdate);
    video.addEventListener("ended", onEnded);
    video.addEventListener("error", onError);

    return () => {
      video.removeEventListener("play", onPlay);
      video.removeEventListener("pause", onPause);
      video.removeEventListener("timeupdate", onTimeUpdate);
      video.removeEventListener("ended", onEnded);
      video.removeEventListener("error", onError);
    };
  }, [itemId, showControls, store]);

  // ─── Progress save interval ──────────────────────────────────────────────

  useEffect(() => {
    progressTimerRef.current = setInterval(() => {
      const video = videoRef.current;
      if (video && !video.paused && video.currentTime > 0) {
        api.updateProgress(itemId, {
          position_ticks: Math.floor(video.currentTime * TICKS_PER_SECOND),
        }).catch(() => {});
      }
    }, PROGRESS_SAVE_INTERVAL);

    return () => clearInterval(progressTimerRef.current);
  }, [itemId]);

  // ─── Fullscreen change listener ──────────────────────────────────────────

  useEffect(() => {
    const onFullscreenChange = () => {
      const fs = !!document.fullscreenElement;
      setIsFullscreen(fs);
      store.getState().setFullscreen(fs);
    };

    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, [store]);

  // ─── Keyboard shortcuts ──────────────────────────────────────────────────

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // Ignore if typing in an input
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement ||
        e.target instanceof HTMLSelectElement
      ) {
        return;
      }

      const video = videoRef.current;
      if (!video) return;

      switch (e.key) {
        case " ":
          e.preventDefault();
          togglePlayPause();
          break;
        case "f":
        case "F":
          e.preventDefault();
          handleToggleFullscreen();
          break;
        case "m":
        case "M":
          e.preventDefault();
          handleToggleMute();
          break;
        case "ArrowLeft":
          e.preventDefault();
          video.currentTime = Math.max(0, video.currentTime - 10);
          showControls();
          break;
        case "ArrowRight":
          e.preventDefault();
          video.currentTime = Math.min(video.duration || 0, video.currentTime + 10);
          showControls();
          break;
        case "ArrowUp":
          e.preventDefault();
          handleVolumeChange(video.volume + 0.05);
          showControls();
          break;
        case "ArrowDown":
          e.preventDefault();
          handleVolumeChange(video.volume - 0.05);
          showControls();
          break;
        case "Escape":
          e.preventDefault();
          if (document.fullscreenElement) {
            document.exitFullscreen().catch(() => {});
          } else {
            handleClose();
          }
          break;
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    togglePlayPause,
    handleToggleFullscreen,
    handleToggleMute,
    handleVolumeChange,
    handleClose,
    showControls,
  ]);

  // ─── Cleanup on unmount ──────────────────────────────────────────────────

  useEffect(() => {
    return () => {
      clearTimeout(hideTimerRef.current);
      clearInterval(progressTimerRef.current);

      // Save final progress
      const video = videoRef.current;
      if (video && video.currentTime > 0) {
        api.updateProgress(itemId, {
          position_ticks: Math.floor(video.currentTime * TICKS_PER_SECOND),
        }).catch(() => {});
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ─── Render ──────────────────────────────────────────────────────────────

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 bg-black select-none"
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
      onClick={togglePlayPause}
    >
      {/* Video element */}
      <video
        ref={videoRef}
        className="absolute inset-0 w-full h-full object-contain"
        playsInline
        onClick={(e) => e.stopPropagation()}
        onDoubleClick={(e) => {
          e.stopPropagation();
          handleToggleFullscreen();
        }}
      />

      {/* Error overlay */}
      {error && (
        <div className="absolute inset-0 flex items-center justify-center z-30 bg-black/80">
          <div className="flex flex-col items-center gap-4 max-w-md px-6 text-center">
            <svg className="h-12 w-12 text-error" viewBox="0 0 24 24" fill="currentColor">
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
              Close Player
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
          currentAudioTrack={currentAudioTrack}
          currentSubtitleTrack={currentSubtitleTrack}
          onPlayPause={togglePlayPause}
          onSeek={handleSeek}
          onVolumeChange={handleVolumeChange}
          onToggleMute={handleToggleMute}
          onToggleFullscreen={handleToggleFullscreen}
          onAudioTrackChange={handleAudioTrackChange}
          onSubtitleTrackChange={handleSubtitleTrackChange}
          onClose={handleClose}
          title={title}
        />
      </div>
    </div>
  );
};

export { VideoPlayer };
export type { VideoPlayerProps };
