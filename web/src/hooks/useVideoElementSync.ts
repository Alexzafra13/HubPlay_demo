import { useEffect } from "react";
import type { RefObject } from "react";

interface UseVideoElementSyncOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  volume: number;
  isMuted: boolean;
  playbackRate: number;
  /**
   * Identidad del media stream actual. Cuando cambia (audio swap,
   * direct→transcode, etc.) el `<video>` se remonta a 1× nativamente
   * — re-aplicamos el `playbackRate` elegido para que la preferencia
   * del usuario sobreviva el remount.
   */
  sourceKey: string | null;
}

/**
 * Sincroniza el estado de reproducción externo (volume / mute /
 * playbackRate) al elemento `<video>`. Dos effects separados para
 * que un cambio de URL re-aplique sólo la velocidad y no agite el
 * volumen.
 */
export function useVideoElementSync({
  videoRef,
  volume,
  isMuted,
  playbackRate,
  sourceKey,
}: UseVideoElementSyncOptions): void {
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    // Mutar atributos del HTMLMediaElement es la API estándar
    // para sincronizar audio settings — no es state mutation
    // sino un side-effect sobre el DOM node. El compiler lo
    // detecta como mutación de "props del hook" (videoRef
    // pasado como argumento) pero no lo es semánticamente.
    // eslint-disable-next-line react-compiler/react-compiler
    video.volume = volume;
    video.muted = isMuted;
  }, [videoRef, volume, isMuted]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    // Misma razón que arriba — ver comentario del primer effect.
    video.playbackRate = playbackRate;
  }, [videoRef, playbackRate, sourceKey]);
}
