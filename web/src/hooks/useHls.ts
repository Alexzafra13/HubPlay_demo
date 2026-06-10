import { useEffect, useRef, useState, useCallback } from "react";
import type { RefObject } from "react";
import { useTranslation } from "react-i18next";
import Hls from "hls.js";
import { destroyHlsInstance } from "./hlsLifecycle";

// PB-16: techo de recuperaciones por origen de error. Sin límite, un
// servidor caído dejaba a hls.js reintentando para siempre con el
// overlay de "recovering…" parpadeando — el usuario solo tenía el
// botón de cerrar. Los contadores se resetean en cada FRAG_LOADED
// sano (la sesión volvió a la vida) y en cada cambio de source.
const MAX_NETWORK_RECOVERIES = 3;
const MAX_MEDIA_RECOVERIES = 3;
// Ventana del patrón documentado de hls.js: si el segundo MEDIA_ERROR
// llega a menos de 3s del anterior, recoverMediaError() solo no basta
// — hay que intentar swapAudioCodec() antes del retry.
const MEDIA_SWAP_WINDOW_MS = 3000;

interface AudioTrack {
  id: number;
  name: string;
  lang: string;
}

interface SubtitleTrack {
  id: number;
  name: string;
  lang: string;
}

interface QualityLevel {
  id: number;        // index into hls.levels
  height: number;    // 1080, 720, ...
  bitrate: number;   // bits/sec
  label: string;     // "1080p", "720p", "Source"
}

interface UseHlsOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
  playbackMethod: string;
  sessionToken: string;
  startPosition?: number;
}

interface UseHlsReturn {
  error: string | null;
  audioTracks: AudioTrack[];
  subtitleTracks: SubtitleTrack[];
  qualityLevels: QualityLevel[];
  currentAudioTrack: number;
  currentSubtitleTrack: number;
  /** -1 = auto (ABR). Otherwise an index into qualityLevels. */
  currentQuality: number;
  setAudioTrack: (id: number) => void;
  setSubtitleTrack: (id: number) => void;
  setQuality: (id: number) => void;
}

export function useHls({
  videoRef,
  masterPlaylistUrl,
  directUrl,
  playbackMethod,
  sessionToken,
  startPosition,
}: UseHlsOptions): UseHlsReturn {
  const hlsRef = useRef<Hls | null>(null);
  const { t } = useTranslation();
  // Latest-ref para que un cambio de idioma no re-monte el player.
  const tRef = useRef(t);
  useEffect(() => {
    tRef.current = t;
  }, [t]);

  // Estado de recovery (PB-16). En ref — los handlers de hls.js viven
  // fuera del ciclo de render. `recovering` distingue el toast
  // transitorio del error terminal al limpiar en FRAG_LOADED.
  const recoveryRef = useRef({
    net: 0,
    media: 0,
    lastMediaErrorAt: 0,
    recovering: false,
  });

  const [error, setError] = useState<string | null>(null);
  const [audioTracks, setAudioTracks] = useState<AudioTrack[]>([]);
  const [subtitleTracks, setSubtitleTracks] = useState<SubtitleTrack[]>([]);
  const [qualityLevels, setQualityLevels] = useState<QualityLevel[]>([]);
  const [currentAudioTrack, setCurrentAudioTrack] = useState(0);
  const [currentSubtitleTrack, setCurrentSubtitleTrack] = useState(-1);
  // hls.js convention: -1 means ABR / auto. We expose the same value
  // so the UI doesn't have to translate between "Auto" and a magic
  // index — selecting "Auto" sets it back to -1.
  const [currentQuality, setCurrentQuality] = useState(-1);

  // startPosition is only consumed when we attach a fresh Hls
  // instance, so we read it from a ref rather than including it in
  // the effect's dep list. Otherwise a parent that recomputes the
  // resume seconds on every render (e.g. from a live duration) would
  // tear the player down and reattach mid-playback.
  const startPositionRef = useRef(startPosition);
  // Sync via effect rather than render-phase assignment: ref
  // mutations during render are a React rule violation, even
  // though in practice this one is harmless (we read it from an
  // effect, never from JSX).
  useEffect(() => {
    startPositionRef.current = startPosition;
  }, [startPosition]);

  // Remembers the most recent reliable currentTime so a
  // recoverMediaError / re-attach path can restore the position
  // instead of letting the <video> element snap to 0. Tracked here
  // (not in the parent) because the recovery hooks live next to the
  // hls.js instance below.
  const lastGoodTimeRef = useRef(0);

  const setAudioTrackCb = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.audioTrack = id;
      setCurrentAudioTrack(id);
    }
  }, []);

  const setSubtitleTrackCb = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.subtitleTrack = id;
      setCurrentSubtitleTrack(id);
    }
  }, []);

  const setQualityCb = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (!hls) return;
    // hls.js: writing -1 to currentLevel re-enables ABR; any
    // non-negative value pins the player to that ladder rung. The
    // local mirror is updated optimistically; LEVEL_SWITCHED will
    // refine to the actual level the engine settled on.
    hls.currentLevel = id;
    setCurrentQuality(id);
  }, []);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    // Reset every piece of source-bound state up-front. Without this
    // the player carries the previous file's audio/subtitle/quality
    // ladder into the new attachment for the few hundred ms before
    // MANIFEST_PARSED fires — most visibly, currentAudioTrack would
    // stay at the prior episode's index until the next user action.
    // The set-state-in-effect rule is disabled here because the
    // alternative — keying the whole hook by src — would tear down
    // the hls.js instance on every source change, which is exactly
    // what we want to avoid for next-up auto-advance.
    setError(null);
    setAudioTracks([]);
    setSubtitleTracks([]);
    setQualityLevels([]);
    setCurrentAudioTrack(0);
    setCurrentSubtitleTrack(-1);
    setCurrentQuality(-1);
    lastGoodTimeRef.current = 0;
    recoveryRef.current = { net: 0, media: 0, lastMediaErrorAt: 0, recovering: false };

    // Tear down anything left from a prior attach. The cleanup
    // returned below runs first when deps change, but defending here
    // too keeps strict-mode double-mount and React-18 effect-replay
    // edge cases honest. Also clears <video src> so a transition
    // direct_play → transcode doesn't leave the previous progressive
    // URL hanging on the element. Shared with useLiveHls so a fix
    // to either codepath cannot drift out of sync silently.
    destroyHlsInstance(hlsRef, video);

    const useHlsPlayback =
      playbackMethod === "transcode" || playbackMethod === "direct_stream";

    if (useHlsPlayback && masterPlaylistUrl) {
      const url = sessionToken
        ? `${masterPlaylistUrl}${masterPlaylistUrl.includes("?") ? "&" : "?"}token=${sessionToken}`
        : masterPlaylistUrl;

      if (Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: false,
          startPosition: startPositionRef.current ?? -1,
          // PB-34: el default de hls.js es back-buffer infinito — en
          // una película de 2h a bitrate alto la memoria crece sin
          // límite (móviles/TV boxes). 90s cubre cualquier seek-atrás
          // razonable sin re-fetch.
          backBufferLength: 90,
          // PB-32: en transcode, un seek a zona no codificada obliga
          // al servidor a reiniciar ffmpeg en frío — el primer byte
          // del segmento puede tardar >10s en hardware modesto, y el
          // TTFB default (10s) lo convertía en error fatal + ciclo de
          // recovery para una situación normal. Solo se relaja el
          // primer byte; el techo total de descarga queda igual.
          ...(playbackMethod === "transcode"
            ? {
                fragLoadPolicy: {
                  default: {
                    maxTimeToFirstByteMs: 30000,
                    maxLoadTimeMs: 120000,
                    timeoutRetry: { maxNumRetry: 4, retryDelayMs: 1000, maxRetryDelayMs: 8000 },
                    errorRetry: { maxNumRetry: 6, retryDelayMs: 1000, maxRetryDelayMs: 8000 },
                  },
                },
              }
            : {}),
          // Opt-in verbose logging. Set `window.__hp_debug_hls = true`
          // in DevTools BEFORE opening the player to dump every
          // hls.js decision (level switch, fragment load, error,
          // recovery) to the console. Off by default to keep
          // production console quiet. Used to diagnose the seek
          // cascade reported on 2026-05-08 — without it the only
          // observability of hls.js's internal seeks was indirect
          // (via server-side RestartSessionAt logs).
          debug:
            typeof window !== "undefined" &&
            (window as unknown as { __hp_debug_hls?: boolean }).__hp_debug_hls === true,
          xhrSetup: (xhr) => {
            // Auth is handled via HTTP-only cookies.
            xhr.withCredentials = true;
          },
        });

        hlsRef.current = hls;
        hls.loadSource(url);
        hls.attachMedia(video);

        // After hls.js detaches and re-attaches the media (the
        // recoverMediaError path below), the <video> element can
        // briefly read currentTime=0 before the new buffer is wired
        // up. If we have a remembered good time, push it back so
        // `play()` resumes from where the user actually was — this
        // closes the doc'd "Play after frozen-paused state restarts
        // from frame 0" bug.
        hls.on(Hls.Events.MEDIA_ATTACHED, () => {
          const target = lastGoodTimeRef.current;
          if (target > 1 && video.currentTime < 0.5) {
            video.currentTime = target;
          }
        });

        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          const aTracks: AudioTrack[] = hls.audioTracks.map((t) => ({
            id: t.id,
            name: t.name,
            lang: t.lang || "",
          }));
          setAudioTracks(aTracks);
          setCurrentAudioTrack(hls.audioTrack);

          // Quality ladder. We expose the levels exactly once (the
          // master playlist is parsed before the first segment plays)
          // and rely on LEVEL_SWITCHED to keep currentQuality in sync
          // when ABR or the user moves between rungs.
          const levels: QualityLevel[] = hls.levels.map((l, idx) => ({
            id: idx,
            height: l.height,
            bitrate: l.bitrate,
            label: l.height > 0 ? `${l.height}p` : `${Math.round(l.bitrate / 1000)} kbps`,
          }));
          setQualityLevels(levels);
          setCurrentQuality(hls.currentLevel); // -1 unless the engine pre-pinned

          video.play().catch(() => {});
        });

        hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
          // Mirror hls.js's "we are now on level N" event. Note that
          // even in ABR mode (currentLevel = -1) the engine still
          // emits this with the concrete level it picked. We keep
          // the user's selection (-1 = auto) by reading it back from
          // hls.autoLevelEnabled rather than trusting data.level.
          if (hls.autoLevelEnabled) {
            setCurrentQuality(-1);
          } else {
            setCurrentQuality(data.level);
          }
        });

        hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
          const sTracks: SubtitleTrack[] = hls.subtitleTracks.map((t) => ({
            id: t.id,
            name: t.name,
            lang: t.lang || "",
          }));
          setSubtitleTracks(sTracks);
          setCurrentSubtitleTrack(hls.subtitleTrack);
        });

        hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, (_event, data) => {
          setCurrentAudioTrack(data.id);
        });

        hls.on(Hls.Events.SUBTITLE_TRACK_SWITCH, (_event, data) => {
          setCurrentSubtitleTrack(data.id);
        });

        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (!data.fatal) return;
          const rec = recoveryRef.current;
          // Capture the best-known position BEFORE recovery so the
          // restart starts in the right place even if the recovery
          // path detaches the media element.
          const resumeFrom =
            video.currentTime > 0.5 ? video.currentTime : lastGoodTimeRef.current;
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              // PB-16: acotado. Con el server caído, el retry infinito
              // solo parpadeaba el overlay para siempre.
              if (++rec.net > MAX_NETWORK_RECOVERIES) {
                rec.recovering = false;
                setError(tRef.current("player.errors.networkFatal"));
                hls.destroy();
                return;
              }
              rec.recovering = true;
              setError(tRef.current("player.errors.networkRecovering"));
              // hls.startLoad(timeSec) tells the loader where to
              // resume; without it the loader picks the live edge
              // (irrelevant for VOD) or replays from the start.
              hls.startLoad(resumeFrom > 0 ? resumeFrom : -1);
              break;
            case Hls.ErrorTypes.MEDIA_ERROR: {
              if (++rec.media > MAX_MEDIA_RECOVERIES) {
                rec.recovering = false;
                setError(tRef.current("player.errors.mediaFatal"));
                hls.destroy();
                return;
              }
              // Patrón documentado de hls.js: un segundo MEDIA_ERROR a
              // <3s del anterior significa que recoverMediaError() no
              // bastó — swapAudioCodec() antes del siguiente intento.
              const now = performance.now();
              if (rec.media > 1 && now - rec.lastMediaErrorAt < MEDIA_SWAP_WINDOW_MS) {
                hls.swapAudioCodec();
              }
              rec.lastMediaErrorAt = now;
              rec.recovering = true;
              setError(tRef.current("player.errors.mediaRecovering"));
              hls.recoverMediaError();
              // recoverMediaError preserves <video>.currentTime, but
              // a follow-on detach (swapAudioCodec arriba) can zero
              // it. The MEDIA_ATTACHED handler above will restore
              // from lastGoodTimeRef once the new media source is
              // wired up.
              break;
            }
            default:
              rec.recovering = false;
              setError(tRef.current("player.errors.fatal", { details: data.details }));
              hls.destroy();
              break;
          }
        });

        // Recovery worked — the next fragment loaded clean. Clear the
        // transient toast and reset the budgets: a session that came
        // back to life earns a fresh allowance for the NEXT incident.
        hls.on(Hls.Events.FRAG_LOADED, () => {
          const rec = recoveryRef.current;
          if (rec.recovering) {
            rec.recovering = false;
            rec.net = 0;
            rec.media = 0;
            setError(null);
          }
        });
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        // Native HLS path (Safari/iOS). Mutar `video.src` es la API
        // estándar del HTMLMediaElement — no estamos mutando un valor
        // que React considere inmutable (el ref), sino una propiedad
        // del DOM node. Suprimido narrow para que el compiler no
        // confunda el patrón con state mutation.
        // eslint-disable-next-line react-compiler/react-compiler
        video.src = url;
        video.addEventListener(
          "loadedmetadata",
          () => {
            video.play().catch(() => {});
          },
          { once: true },
        );
      } else {
        setError(tRef.current("player.errors.hlsUnsupported"));
      }
    } else if (playbackMethod === "direct_play" && directUrl) {
      // Auth is handled via HTTP-only cookies for same-origin requests.
      video.src = directUrl;
      video.addEventListener(
        "loadedmetadata",
        () => {
          video.play().catch(() => {});
        },
        { once: true },
      );
    } else {
      setError(tRef.current("player.errors.noSource"));
    }

    // Capture the listener-removal handle in scope: the `onSettledTime`
    // closure is only defined inside the `useHlsPlayback` branch, so
    // the cleanup needs a stable cleanup function regardless of which
    // branch ran.
    const settledListener = () => {
      if (video && !video.seeking && video.currentTime > 0.5) {
        lastGoodTimeRef.current = video.currentTime;
      }
    };
    if (useHlsPlayback && masterPlaylistUrl) {
      // The branch above already attached its own listener, but we
      // still register this one so direct_play paths also benefit
      // from the settled-time tracking — recovery works the same way
      // when a flaky network blanks the <video> source mid-play.
    }
    video.addEventListener("timeupdate", settledListener);

    // Las rutas de src directo (HLS nativo en Safari/iOS y direct_play)
    // no tenían NINGÚN listener de error: un fallo de decode a mitad de
    // reproducción o un fichero corrupto dejaban el overlay de carga
    // girando para siempre, sin mensaje. hls.js gestiona sus propios
    // errores (con recovery) vía Hls.Events.ERROR, así que mientras
    // haya instancia viva no interferimos.
    const onVideoError = () => {
      if (hlsRef.current) return;
      // MediaError.code: 1 aborted, 2 network, 3 decode, 4 src no
      // soportado. El 1 (abort) se ignora — lo dispara el propio
      // teardown/zapping del usuario, no es un fallo.
      const code = video.error?.code;
      if (code === 2) {
        setError(tRef.current("player.errors.videoNetwork"));
      } else if (code === 3) {
        setError(tRef.current("player.errors.videoDecode"));
      } else if (code === 4) {
        setError(tRef.current("player.errors.videoFormat"));
      } else if (code !== 1) {
        setError(tRef.current("player.errors.videoUnknown"));
      }
    };
    video.addEventListener("error", onVideoError);

    return () => {
      video.removeEventListener("error", onVideoError);
      destroyHlsInstance(hlsRef, video);
      video.removeEventListener("timeupdate", settledListener);
    };
  }, [videoRef, masterPlaylistUrl, directUrl, playbackMethod, sessionToken]);

  return {
    error,
    audioTracks,
    subtitleTracks,
    qualityLevels,
    currentAudioTrack,
    currentSubtitleTrack,
    currentQuality,
    setAudioTrack: setAudioTrackCb,
    setSubtitleTrack: setSubtitleTrackCb,
    setQuality: setQualityCb,
  };
}
