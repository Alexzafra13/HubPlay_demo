import { useCallback } from "react";
import type { RefObject } from "react";

interface UsePlayerActionsOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  containerRef: RefObject<HTMLDivElement | null>;
  /** True en viewport mobile — cambia el comportamiento del tap sobre la superficie. */
  isMobile: boolean;
  /** Estado de visibilidad actual de los controles (para el fork del tap mobile). */
  controlsVisible: boolean;
  showControls: () => void;
  hideControls: () => void;
  /** Mute actual del store (para el handleVolumeChange — al subir de 0 desmutea). */
  isMuted: boolean;
  setVolume: (v: number) => void;
  toggleMute: () => void;
  /** Callback del caller que cierra el player (lo usa handleClose). */
  onClose: () => void;
}

export interface PlayerActions {
  togglePlayPause: () => void;
  handleSurfaceTap: () => void;
  handleSeek: (time: number) => void;
  handleVolumeChange: (v: number) => void;
  handleToggleMute: () => void;
  handleToggleFullscreen: () => void;
  handleClose: () => void;
  handleTogglePiP: () => Promise<void>;
}

/**
 * Acciones del player que cualquier caller (controles UI, atajos
 * de teclado, gestos táctiles) puede invocar sin conocer la
 * implementación. Encapsular aquí evita duplicar la lógica
 * (p. ej. la pre-flight de PiP) y mantiene `VideoPlayer.tsx`
 * libre de useCallbacks colgando entre effects.
 */
export function usePlayerActions({
  videoRef,
  containerRef,
  isMobile,
  controlsVisible,
  showControls,
  hideControls,
  isMuted,
  setVolume,
  toggleMute,
  onClose,
}: UsePlayerActionsOptions): PlayerActions {
  const togglePlayPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      video.play().catch(() => {});
    } else {
      video.pause();
    }
  }, [videoRef]);

  // Tap sobre la superficie: en mobile sólo alterna visibilidad
  // de controles (sin pausa accidental cuando el usuario sólo
  // quiere ver la barra); en desktop cae a togglePlayPause —
  // la convención de ratón es click-to-pause. La decisión se
  // toma al click, no via handlers distintos, así un resize que
  // flipea isMobile mid-session mantiene el comportamiento
  // consistente.
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

  const handleSeek = useCallback(
    (time: number) => {
      const video = videoRef.current;
      if (!video) return;
      // eslint-disable-next-line react-compiler/react-compiler
      video.currentTime = time;
    },
    [videoRef],
  );

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
  }, [containerRef]);

  const handleClose = useCallback(() => {
    if (document.fullscreenElement) {
      document.exitFullscreen().then(() => onClose()).catch(() => onClose());
    } else {
      onClose();
    }
  }, [onClose]);

  // PiP toggle. Pre-flight failures (no <video>, user-gesture
  // missing, browser sin PiP) son no-fatal — silently no-op en
  // vez de tirar excepción en la cara del usuario; el operador
  // sigue teniendo fullscreen como fallback.
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
      // Ignorado — pre-flight + browser-policy errors son
      // recuperables si el usuario reintenta desde un gesture.
    }
  }, [videoRef]);

  return {
    togglePlayPause,
    handleSurfaceTap,
    handleSeek,
    handleVolumeChange,
    handleToggleMute,
    handleToggleFullscreen,
    handleClose,
    handleTogglePiP,
  };
}
