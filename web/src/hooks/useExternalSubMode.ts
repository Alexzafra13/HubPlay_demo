import { useEffect } from "react";
import type { RefObject } from "react";

interface UseExternalSubModeOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  /**
   * Identidad del subtítulo externo activo. Cualquier valor truthy
   * dispara la activación (clave estable preferida — pasa
   * `pick.file_id` o similar para que el effect se re-ejecute al
   * cambiar de track sin re-renders extra). `null` / `undefined`
   * → no-op (no hay sub externo activo).
   */
  activeKey: string | null | undefined;
  /**
   * Prefijo del `label` que identifica el `<track>` a activar.
   * Default "External:" (OpenSubtitles); el carril de texto local
   * embebido usa "Local:".
   */
  labelPrefix?: string;
}

/**
 * Fuerza `track.mode = "showing"` sobre el `<track>` externo recién
 * insertado y suprime cualquier otro track ya en showing.
 *
 * Tras montar un `<track>` el navegador lo deja en `disabled` por
 * defecto — no se renderiza hasta que alguien lo flippa. Esperamos
 * un rAF antes de tocar el mode porque el DOM puede no haber
 * aplicado el nuevo elemento en el microtask inmediato.
 *
 * Identificamos el track externo por el prefijo `"External:"` en
 * su `label` (convención establecida por el JSX que monta el
 * `<track>` en VideoPlayer).
 */
export function useExternalSubMode({
  videoRef,
  activeKey,
  labelPrefix = "External:",
}: UseExternalSubModeOptions): void {
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !activeKey) return;
    const rafID = window.requestAnimationFrame(() => {
      const tracks = Array.from(video.textTracks);
      const target = tracks.find((t) => t.label.startsWith(labelPrefix));
      if (target) target.mode = "showing";
      // Suprime cualquier otro track en showing para no
      // doble-renderizar cues de un sub HLS pre-existente.
      for (const t of tracks) {
        if (t !== target && t.mode === "showing") {
          t.mode = "disabled";
        }
      }
    });
    return () => window.cancelAnimationFrame(rafID);
  }, [videoRef, activeKey, labelPrefix]);
}
