import { useEffect } from "react";
import type { RefObject } from "react";

interface UsePlayerKeyboardOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  onTogglePlay: () => void;
  onToggleFullscreen: () => void;
  onToggleMute: () => void;
  onVolumeChange: (v: number) => void;
  onClose: () => void;
  onActivity: () => void;
  /** Optional. Toggle Picture-in-Picture (`p`). Hook silently
   *  skips the binding when the caller doesn't pass a handler so
   *  the surface stays backward-compatible with embeds that don't
   *  surface a PiP affordance. */
  onTogglePiP?: () => void;
  /** Optional. Toggle the help overlay (`?`). Same backward-compat
   *  rationale as onTogglePiP. */
  onToggleHelp?: () => void;
}

export function usePlayerKeyboard({
  videoRef,
  onTogglePlay,
  onToggleFullscreen,
  onToggleMute,
  onVolumeChange,
  onClose,
  onActivity,
  onTogglePiP,
  onToggleHelp,
}: UsePlayerKeyboardOptions): void {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement ||
        e.target instanceof HTMLSelectElement
      ) {
        return;
      }

      const video = videoRef.current;
      if (!video) return;

      // Number keys 0..9 jump to N*10% of the duration — the
      // YouTube / Plex / VLC convention. Branch separately because
      // the switch below would need 10 cases of identical shape.
      if (/^[0-9]$/.test(e.key) && video.duration > 0) {
        e.preventDefault();
        const pct = parseInt(e.key, 10) / 10;
        video.currentTime = video.duration * pct;
        onActivity();
        return;
      }

      switch (e.key) {
        case " ":
        case "k": // YouTube convention — same as space
        case "K":
          e.preventDefault();
          onTogglePlay();
          break;
        case "f":
        case "F":
          e.preventDefault();
          onToggleFullscreen();
          break;
        case "m":
        case "M":
          e.preventDefault();
          onToggleMute();
          break;
        case "ArrowLeft":
        case "j": // YouTube convention — same as ArrowLeft
        case "J":
          e.preventDefault();
          video.currentTime = Math.max(0, video.currentTime - 10);
          onActivity();
          break;
        case "ArrowRight":
        case "l": // YouTube convention — same as ArrowRight
        case "L":
          e.preventDefault();
          video.currentTime = Math.min(
            video.duration || 0,
            video.currentTime + 10,
          );
          onActivity();
          break;
        case "ArrowUp":
          e.preventDefault();
          onVolumeChange(video.volume + 0.05);
          onActivity();
          break;
        case "ArrowDown":
          e.preventDefault();
          onVolumeChange(video.volume - 0.05);
          onActivity();
          break;
        case "p":
        case "P":
          if (onTogglePiP) {
            e.preventDefault();
            onTogglePiP();
          }
          break;
        case "?":
          if (onToggleHelp) {
            e.preventDefault();
            onToggleHelp();
          }
          break;
        case "Escape":
          e.preventDefault();
          if (document.fullscreenElement) {
            document.exitFullscreen().catch(() => {});
          } else {
            onClose();
          }
          break;
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    videoRef,
    onTogglePlay,
    onToggleFullscreen,
    onToggleMute,
    onVolumeChange,
    onClose,
    onActivity,
    onTogglePiP,
    onToggleHelp,
  ]);
}
