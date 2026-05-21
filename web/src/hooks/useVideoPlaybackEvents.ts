import { useEffect, useReducer, useRef } from "react";
import type { RefObject } from "react";
import { api } from "@/api/client";

export interface PlaybackSnapshot {
  isPlaying: boolean;
  currentTime: number;
  duration: number;
  buffered: number;
  firstFrameReady: boolean;
}

type PlaybackAction =
  | { type: "play" }
  | { type: "playing" }
  | { type: "pause" }
  | { type: "ended" }
  | { type: "seeked"; currentTime: number }
  | {
      type: "timeUpdate";
      currentTime: number;
      duration: number;
      buffered: number;
      seeking: boolean;
    }
  | { type: "sourceChanged" };

const INITIAL: PlaybackSnapshot = {
  isPlaying: false,
  currentTime: 0,
  duration: 0,
  buffered: 0,
  firstFrameReady: false,
};

function playbackReducer(
  state: PlaybackSnapshot,
  action: PlaybackAction,
): PlaybackSnapshot {
  switch (action.type) {
    case "play":
      return state.isPlaying ? state : { ...state, isPlaying: true };
    case "playing":
      return state.firstFrameReady ? state : { ...state, firstFrameReady: true };
    case "pause":
    case "ended":
      return state.isPlaying ? { ...state, isPlaying: false } : state;
    case "seeked":
      return state.currentTime === action.currentTime
        ? state
        : { ...state, currentTime: action.currentTime };
    case "timeUpdate": {
      // currentTime sólo se actualiza cuando NO hay seek en vuelo —
      // la fuente de verdad es `video.seeking`, no eventos (un
      // `seeked` puede caerse y dejar un ref pegado). duration y
      // buffered sí se refrescan siempre.
      const cur = action.seeking ? state.currentTime : action.currentTime;
      if (
        state.currentTime === cur &&
        state.duration === action.duration &&
        state.buffered === action.buffered
      ) {
        return state;
      }
      return {
        ...state,
        currentTime: cur,
        duration: action.duration,
        buffered: action.buffered,
      };
    }
    case "sourceChanged":
      return { ...INITIAL };
    default:
      return state;
  }
}

interface UseVideoPlaybackEventsOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  itemId: string;
  knownDuration?: number;
  /** Notificación a cada timeupdate (incluido durante seek). */
  onProgress?: (
    currentTime: number,
    duration: number,
    buffered: number,
  ) => void;
  /**
   * Disparado al alcanzar fin de stream. El padre decide UI gating
   * (up-next overlay vs auto-advance inmediato).
   */
  onEnded?: () => void;
  /** Revelar controles (play / first-frame). */
  onActivity: () => void;
  /** Mantener controles visibles (pause / ended — sin auto-hide). */
  onSettled: () => void;
}

/**
 * Consolida el estado de playback (antes 5 useState distintos) y la
 * cascada de setState en el handler de eventos del <video> en un único
 * useReducer + un solo effect de suscripción.
 *
 * Antes: el effect principal de VideoPlayer tenía 6 listeners con 9
 * setState distribuidos, y deps `[itemId, knownDuration, showControls,
 * keepControlsVisible, updateTime, nextUp]`. Cada cambio de identidad
 * de cualquiera (especialmente showControls que cambia al togglear
 * isPlaying en useControlsVisibility) re-suscribía los 6 listeners,
 * con riesgo de perder eventos durante el churn.
 *
 * Ahora: deps `[videoRef, itemId]`. Los callbacks se leen via refs
 * latest-value (patrón pragmático del repo en lugar de useEffectEvent
 * que aún es experimental). Cada timeupdate dispatch un único action
 * con short-circuit si nada cambió.
 */
export function useVideoPlaybackEvents({
  videoRef,
  itemId,
  knownDuration,
  onProgress,
  onEnded,
  onActivity,
  onSettled,
}: UseVideoPlaybackEventsOptions): PlaybackSnapshot {
  const [state, dispatch] = useReducer(playbackReducer, INITIAL);

  const onProgressRef = useRef(onProgress);
  const onEndedRef = useRef(onEnded);
  const onActivityRef = useRef(onActivity);
  const onSettledRef = useRef(onSettled);
  const knownDurationRef = useRef(knownDuration);
  useEffect(() => {
    onProgressRef.current = onProgress;
    onEndedRef.current = onEnded;
    onActivityRef.current = onActivity;
    onSettledRef.current = onSettled;
    knownDurationRef.current = knownDuration;
  });

  // Posición fiable más reciente (timeupdate sin seek). Recupera del
  // edge case "Play after pause restarts from frame 0" tras
  // recoverMediaError o detach/reattach que zerea el currentTime.
  const lastGoodTimeRef = useRef(0);

  // Reset al cambiar `itemId` (auto-advance entre episodios sin remontar
  // hls.js). El padre NO debería usar `key={itemId}` porque tirar la
  // instancia es justo lo que auto-advance evita.
  useEffect(() => {
    dispatch({ type: "sourceChanged" });
    lastGoodTimeRef.current = 0;
  }, [itemId]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const onPlay = () => {
      dispatch({ type: "play" });
      onActivityRef.current();
      if (video.currentTime < 1 && lastGoodTimeRef.current > 1) {
        video.currentTime = lastGoodTimeRef.current;
      }
    };

    const onPlaying = () => dispatch({ type: "playing" });

    const onPause = () => {
      dispatch({ type: "pause" });
      onSettledRef.current();
    };

    const onSeeked = () => {
      dispatch({ type: "seeked", currentTime: video.currentTime });
    };

    const onTimeUpdate = () => {
      const videoDur = video.duration;
      const known = knownDurationRef.current;
      const effectiveDuration =
        known && known > 0
          ? known
          : videoDur && isFinite(videoDur) && videoDur > 0
            ? videoDur
            : 0;
      const buffered =
        video.buffered.length > 0
          ? video.buffered.end(video.buffered.length - 1)
          : 0;
      const seeking = video.seeking;

      dispatch({
        type: "timeUpdate",
        currentTime: video.currentTime,
        duration: effectiveDuration,
        buffered,
        seeking,
      });

      if (!seeking && video.currentTime > 0.5) {
        lastGoodTimeRef.current = video.currentTime;
      }

      onProgressRef.current?.(video.currentTime, effectiveDuration, buffered);
    };

    const onEndedHandler = () => {
      dispatch({ type: "ended" });
      onSettledRef.current();
      api.markPlayed(itemId).catch(() => {});
      onEndedRef.current?.();
    };

    video.addEventListener("play", onPlay);
    video.addEventListener("playing", onPlaying);
    video.addEventListener("pause", onPause);
    video.addEventListener("seeked", onSeeked);
    video.addEventListener("timeupdate", onTimeUpdate);
    video.addEventListener("ended", onEndedHandler);

    return () => {
      video.removeEventListener("play", onPlay);
      video.removeEventListener("playing", onPlaying);
      video.removeEventListener("pause", onPause);
      video.removeEventListener("seeked", onSeeked);
      video.removeEventListener("timeupdate", onTimeUpdate);
      video.removeEventListener("ended", onEndedHandler);
    };
  }, [videoRef, itemId]);

  return state;
}
