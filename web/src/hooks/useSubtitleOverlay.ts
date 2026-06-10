import { useEffect } from "react";
import type { RefObject } from "react";

/**
 * Prefijos de label de los `<track>` que HubPlay gestiona (texto local
 * extraído a WebVTT y subtítulos federados de un peer).
 */
const MANAGED_PREFIXES = ["Local:", "Federated:"];

interface UseSubtitleOverlayOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  /** Contenedor donde se pintan los cues activos. */
  overlayRef: RefObject<HTMLDivElement | null>;
  /** Identidad del sub activo; null = sin subtítulos gestionados.
   *  Cambiarla re-arma el effect (nueva pista → nuevo binding). */
  activeKey: string | null;
}

/**
 * Render propio de subtítulos (PB-44, reporte de usuario 2026-06-10).
 *
 * El render NATIVO de WebVTT pinta los cues en el borde inferior del
 * ELEMENTO <video> — que en nuestro player ocupa la pantalla entera,
 * no el área visible de la película (object-contain). En móvil eso
 * significaba subtítulos pisando la barra de controles, recortados por
 * el borde físico (sin safe-area), cues simultáneos solapándose y
 * posiciones rotas al girar el dispositivo. Ninguna de esas cosas es
 * controlable desde CSS estándar (::cue no permite reposicionar).
 *
 * Patrón Jellyfin/Plex web: la pista activa va en modo "hidden" — el
 * navegador sigue cargando el fichero, poblando activeCues y
 * disparando `cuechange`, pero NO pinta nada — y los cues se renderizan
 * en un overlay propio (posicionado por VideoPlayer con safe-area y
 * consciente de si los controles están visibles).
 *
 * La mutación del DOM va directa al contenedor (replaceChildren) en
 * vez de pasar por estado React: los cuechange llegan varias veces
 * por segundo y no deben provocar renders del árbol del player.
 */
export function useSubtitleOverlay({
  videoRef,
  overlayRef,
  activeKey,
}: UseSubtitleOverlayOptions): void {
  useEffect(() => {
    const video = videoRef.current;
    const overlay = overlayRef.current;
    if (!video || !overlay) return;

    const clear = () => overlay.replaceChildren();

    if (!activeKey) {
      clear();
      return;
    }

    let target: TextTrack | null = null;

    const renderCues = () => {
      clear();
      const cues = target?.activeCues;
      if (!cues) return;
      for (const cue of Array.from(cues)) {
        const line = document.createElement("div");
        line.className = "hp-cue";
        const vtt = cue as VTTCue;
        if (typeof vtt.getCueAsHTML === "function") {
          // Fragmento inerte parseado por el navegador: respeta <i>/<b>
          // del VTT sin riesgo de inyección (no es innerHTML).
          line.append(vtt.getCueAsHTML());
        } else {
          line.textContent = vtt.text ?? "";
        }
        overlay.append(line);
      }
    };

    const attach = () => {
      const tracks = Array.from(video.textTracks);
      target =
        tracks.find((t) =>
          MANAGED_PREFIXES.some((p) => t.label.startsWith(p)),
        ) ?? null;
      // Cualquier otro track en showing duplicaría cues (el origen del
      // solapamiento de la captura del usuario).
      for (const t of tracks) {
        if (t !== target && t.mode === "showing") {
          t.mode = "disabled";
        }
      }
      if (!target) {
        clear();
        return;
      }
      target.mode = "hidden";
      target.addEventListener("cuechange", renderCues);
      renderCues();
    };

    // rAF: el <track> recién montado aún no aparece en textTracks en
    // el microtask inmediato (mismo motivo que el viejo
    // useExternalSubMode al que este hook sustituye).
    const rafID = window.requestAnimationFrame(attach);

    return () => {
      window.cancelAnimationFrame(rafID);
      target?.removeEventListener("cuechange", renderCues);
      clear();
    };
  }, [videoRef, overlayRef, activeKey]);
}
