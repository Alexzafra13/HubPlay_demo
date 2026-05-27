import { useEffect, useRef } from "react";
import type { RefObject } from "react";

interface UseStartPositionSeekOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  /**
   * Seconds to seek to on first `canplay`. Falsy / 0 → no seek.
   * Solo aplica para direct_play; los transcodes traen el offset
   * baked-in vía `?start=N` en el master.m3u8.
   */
  startPosition: number | undefined;
  /**
   * Identidad del media stream. Cuando cambia (audio swap →
   * master.m3u8 con nuevo ?audio=N) se resetea el gate para que
   * el siguiente canplay re-seekee al startPosition actualizado.
   * Sin esto, cambiar de dub mientras reproduces te dejaba en
   * frame 0 del nuevo transcode (el ref ya estaba a true).
   */
  sourceKey: string | null;
}

/**
 * Aplica un seek inicial a `startPosition` en el primer evento
 * `canplay` del `<video>`. Sin gate, el listener volvería a
 * dispararse cada vez que el buffer recupera y nos teletransportaría
 * al startPosition cada canplay. Con el gate, se aplica una vez por
 * sourceKey y se resetea cuando el stream cambia.
 */
export function useStartPositionSeek({
  videoRef,
  startPosition,
  sourceKey,
}: UseStartPositionSeekOptions): void {
  const seekedRef = useRef(false);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !startPosition || seekedRef.current) return;

    const onCanPlay = () => {
      if (!seekedRef.current && startPosition > 0) {
        video.currentTime = startPosition;
        seekedRef.current = true;
      }
    };

    video.addEventListener("canplay", onCanPlay);
    return () => video.removeEventListener("canplay", onCanPlay);
  }, [videoRef, startPosition]);

  useEffect(() => {
    seekedRef.current = false;
  }, [sourceKey]);
}
