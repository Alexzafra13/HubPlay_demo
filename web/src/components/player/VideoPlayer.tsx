import { useEffect, useRef, useState, useCallback } from "react";
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
import { useVideoPlaybackEvents } from "@/hooks/useVideoPlaybackEvents";
import { useFederatedSubs } from "@/hooks/useFederatedSubs";
import { usePlayerOverlays } from "@/hooks/usePlayerOverlays";
import { useExternalSubMode } from "@/hooks/useExternalSubMode";
import { PlayerControls } from "./PlayerControls";
import { UpNextOverlay, type UpNextInfo } from "./UpNextOverlay";
import { ExternalSubsModal } from "./ExternalSubsModal";
import { KeyboardHelpOverlay } from "./KeyboardHelpOverlay";
import { SkipSegmentButton } from "./SkipSegmentButton";
import { BackdropLoadingOverlay } from "./BackdropLoadingOverlay";
import { ErrorOverlay } from "./ErrorOverlay";
import { useSubtitleSelection } from "@/hooks/useSubtitleSelection";
import { useAudioSelection } from "@/hooks/useAudioSelection";
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

  // Zustand as single source of truth for volume/mute/fullscreen
  const volume = usePlayerStore((s) => s.volume);
  const isMuted = usePlayerStore((s) => s.isMuted);
  const isFullscreen = usePlayerStore((s) => s.isFullscreen);
  const setVolume = usePlayerStore((s) => s.setVolume);
  const toggleMute = usePlayerStore((s) => s.toggleMute);
  const setFullscreen = usePlayerStore((s) => s.setFullscreen);
  const updateTime = usePlayerStore((s) => s.updateTime);

  const {
    upNextActive,
    externalSubsModalOpen,
    activeExternalSub,
    showHelp,
    handleEnded: handleVideoEnded,
    handleUpNextConfirm,
    handleUpNextCancel,
    openExternalSubsModal,
    closeExternalSubsModal,
    pickExternalSub,
    clearExternalSub,
    toggleHelp,
    closeHelp,
  } = usePlayerOverlays({
    itemId,
    hasNextUp: !!nextUp,
    onEndedCallback: onEndedCallback,
  });

  // Subtítulos federados: el fetch, estado del track activo y el effect
  // que fuerza `track.mode = "showing"` viven en useFederatedSubs. IDs
  // ≥ FEDERATED_TRACK_ID_BASE en `mergedSubtitleTracks` discriminan los
  // federados de los HLS-native al despachar en handleSubtitleTrackChange.
  const {
    federatedSubs,
    activeFederatedSubIndex,
    setActiveFederatedSubIndex,
  } = useFederatedSubs({ videoRef, peerId, peerStreamSessionId });

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

  // Forward ref para romper la dependencia circular entre
  // useVideoPlaybackEvents (necesita onActivity/onSettled) y
  // useControlsVisibility (necesita isPlaying). Se rellena en un
  // useEffect post-commit con las funciones reales; los listeners de
  // <video> leen `controlsRef.current.*` en cada disparo, así que para
  // cuando el usuario interactúa siempre apuntan a las versiones
  // reales (no a los noops del bootstrap inicial).
  const controlsRef = useRef<{
    showControls: () => void;
    keepControlsVisible: () => void;
  }>({ showControls: () => {}, keepControlsVisible: () => {} });

  const {
    isPlaying,
    currentTime,
    duration,
    buffered,
    firstFrameReady,
  } = useVideoPlaybackEvents({
    videoRef,
    itemId,
    knownDuration,
    onProgress: updateTime,
    onEnded: handleVideoEnded,
    onActivity: () => controlsRef.current.showControls(),
    onSettled: () => controlsRef.current.keepControlsVisible(),
  });

  const {
    controlsVisible,
    showControls,
    hideControls,
    handleMouseMove,
    handleMouseLeave,
    keepControlsVisible,
  } = useControlsVisibility(isPlaying);

  useEffect(() => {
    controlsRef.current.showControls = showControls;
    controlsRef.current.keepControlsVisible = keepControlsVisible;
  }, [showControls, keepControlsVisible]);

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

  // External subs lifecycle.
  // - Opening the modal is a single setter; closing too.
  // - Picking a result: stash it as state so the JSX renders a fresh
  //   <track>. Suppress any HLS subtitle that might be active so the
  //   two systems don't race over which cues to show.
  const handleExternalSubPicked = useCallback(
    (pick: ExternalSubtitleResult) => {
      pickExternalSub(pick);
      setSubtitleTrack(-1);
      setActiveFederatedSubIndex(null);
    },
    [pickExternalSub, setSubtitleTrack, setActiveFederatedSubIndex],
  );

  // Tras montar el <track> externo, fuerza su mode a "showing" en
  // el siguiente rAF (el DOM aún no tiene el elemento en el
  // microtask inmediato) y suprime cualquier otro track en showing
  // para no doble-renderizar cues de un HLS sub pre-existente.
  useExternalSubMode({
    videoRef,
    activeKey: activeExternalSub
      ? `${activeExternalSub.source}:${activeExternalSub.file_id}`
      : null,
  });

  const {
    mergedSubtitleTracks,
    effectiveCurrentSubtitleTrack,
    handleSubtitleTrackChange,
  } = useSubtitleSelection({
    videoRef,
    hlsTracks: subtitleTracks,
    currentHlsTrack: currentSubtitleTrack,
    setHlsTrack: setSubtitleTrack,
    peerId,
    peerStreamSessionId,
    federatedSubs,
    activeFederatedSubIndex,
    setActiveFederatedSubIndex,
    subtitleStreams,
    burnSubtitleIndex,
    onBurnSubtitleSelected,
    clearActiveExternalSub: clearExternalSub,
  });

  const {
    displayAudioTracks,
    displayCurrentAudioTrack,
    useDbAudioPicker,
    handleAudioTrackChange,
  } = useAudioSelection({
    videoRef,
    i18n,
    hlsAudioTracks: audioTracks,
    currentHlsAudioTrack: currentAudioTrack,
    setHlsAudioTrack: setAudioTrack,
    audioStreams,
    audioStreamIndex,
    onAudioStreamSelected,
  });

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

      <BackdropLoadingOverlay
        firstFrameReady={firstFrameReady}
        backdropUrl={backdropUrl}
        logoUrl={logoUrl}
        title={title}
      />

      {error && (
        <ErrorOverlay
          message={error}
          closeLabel={t("playerControls.closePlayer")}
          onClose={handleClose}
        />
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
          onSearchExternalSubs={openExternalSubsModal}
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
          onClose={closeExternalSubsModal}
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
        <KeyboardHelpOverlay onClose={closeHelp} />
      )}
    </div>
  );
};

export { VideoPlayer };
