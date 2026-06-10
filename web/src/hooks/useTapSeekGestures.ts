import { useCallback, useEffect, useRef } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";

/**
 * Ventana de doble-tap. 300ms es el estándar de facto táctil: más
 * corto pierde dobles legítimos en pantallas grandes, más largo hace
 * perezoso el toggle de controles del tap simple.
 */
const DOUBLE_TAP_WINDOW_MS = 300;

/** Fracción del ancho que cuenta como zona lateral de salto. */
const SKIP_ZONE = 0.34;

type TapZone = "back" | "center" | "fwd";

interface UseTapSeekGesturesOptions {
  isMobile: boolean;
  /** Acción del tap simple (toggle de controles en móvil, play/pause
   *  en desktop). En móvil se difiere DOUBLE_TAP_WINDOW_MS para poder
   *  distinguirla del doble-tap; en desktop dispara inmediato (allí el
   *  doble-click ya tiene dueño: fullscreen sobre el <video>). */
  onSingleTap: () => void;
  /** Doble-tap en una zona lateral → salto en esa dirección. Taps
   *  sucesivos dentro de la ventana encadenan saltos (manteniendo el
   *  ritmo del pulgar, sin re-esperar un doble completo). */
  onZoneSkip: (dir: "back" | "fwd") => void;
}

/**
 * Gestos de tap sobre la superficie del player. En móvil táctil, el
 * doble-tap en el tercio izquierdo/derecho salta ∓10s (convención
 * Netflix/YouTube — sin esto la ÚNICA forma de saltar en touch era
 * arrastrar la seek bar). El tap simple conserva su rol de siempre.
 */
export function useTapSeekGestures({
  isMobile,
  onSingleTap,
  onZoneSkip,
}: UseTapSeekGesturesOptions) {
  const lastTapRef = useRef<{ at: number; zone: TapZone } | null>(null);
  const singleTimerRef = useRef<number | null>(null);

  // Latest-refs: los callbacks del padre cambian de identidad por
  // render; el timer diferido debe ver siempre la versión actual.
  const onSingleTapRef = useRef(onSingleTap);
  const onZoneSkipRef = useRef(onZoneSkip);
  useEffect(() => {
    onSingleTapRef.current = onSingleTap;
    onZoneSkipRef.current = onZoneSkip;
  }, [onSingleTap, onZoneSkip]);

  useEffect(
    () => () => {
      if (singleTimerRef.current !== null) {
        window.clearTimeout(singleTimerRef.current);
      }
    },
    [],
  );

  const handleSurfaceClick = useCallback(
    (e: ReactMouseEvent<HTMLElement>) => {
      if (!isMobile) {
        onSingleTapRef.current();
        return;
      }

      const rect = e.currentTarget.getBoundingClientRect();
      const x = rect.width > 0 ? (e.clientX - rect.left) / rect.width : 0.5;
      const zone: TapZone =
        x < SKIP_ZONE ? "back" : x > 1 - SKIP_ZONE ? "fwd" : "center";
      const now = performance.now();
      const prev = lastTapRef.current;
      lastTapRef.current = { at: now, zone };

      const isChainedSkip =
        prev !== null &&
        now - prev.at < DOUBLE_TAP_WINDOW_MS &&
        zone !== "center" &&
        prev.zone === zone;

      if (isChainedSkip) {
        if (singleTimerRef.current !== null) {
          window.clearTimeout(singleTimerRef.current);
          singleTimerRef.current = null;
        }
        onZoneSkipRef.current(zone);
        return;
      }

      // Primer tap: diferir el single para poder cancelarlo si llega
      // el segundo dentro de la ventana.
      if (singleTimerRef.current !== null) {
        window.clearTimeout(singleTimerRef.current);
      }
      singleTimerRef.current = window.setTimeout(() => {
        singleTimerRef.current = null;
        onSingleTapRef.current();
      }, DOUBLE_TAP_WINDOW_MS);
    },
    [isMobile],
  );

  return { handleSurfaceClick };
}
