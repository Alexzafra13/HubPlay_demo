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
}

export function usePlayerKeyboard({
  videoRef,
  onTogglePlay,
  onToggleFullscreen,
  onToggleMute,
  onVolumeChange,
  onClose,
  onActivity,
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

      switch (e.key) {
        case " ":
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
          e.preventDefault();
          video.currentTime = Math.max(0, video.currentTime - 10);
          onActivity();
          break;
        case "ArrowRight":
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
  ]);
}
