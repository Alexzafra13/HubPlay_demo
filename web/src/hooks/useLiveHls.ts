import { useCallback, useEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import Hls from "hls.js";

/**
 * useLiveHls plays a live IPTV stream from a direct URL.
 *
 * Separate from the on-demand `useHls` hook because Live TV has no session
 * token, no direct-play fallback to a transcoded file, and a different error
 * strategy (retry network errors → fall back to native <video src>).
 *
 * Returns `{ error, loading, reload }`. Call `reload` to re-attach and retry;
 * the hook also re-runs whenever `streamUrl` changes, so just pass a new URL
 * to zap to a different channel — the old instance is cleaned up automatically.
 */
interface UseLiveHlsOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  streamUrl: string | null;
  unavailableMessage: string;
  timeoutMs?: number;
  /**
   * Called exactly once per streamUrl (not per resume / per re-attach)
   * when playback actually starts producing frames. The "continue
   * watching" beacon hangs off here — pause-and-resume must not bump
   * the history timestamp (would trivially defeat the
   * most-recent-first rail). Fires *after* the first `playing` event
   * of a fresh attachment; subsequent plays against the same URL are
   * no-ops even if the user pauses and resumes many times.
   *
   * StreamPreview (hover) uses hls.js directly without this hook, so
   * plugging the beacon here naturally excludes preview playback —
   * exactly the desired semantics.
   */
  onFirstPlay?: () => void;
  /**
   * Called when hls.js gives up on a fatal error (after retries) and
   * we fall back to native playback or surface the error to the user.
   * Used to fire the playback-failure beacon so the channel-health
   * system can see client-side dead-stream signal that the proxy
   * can't observe (manifest 200 OK + dead segments).
   *
   * Fired at most once per streamUrl — repeated fatal events on the
   * same attachment are suppressed so a flapping player can't
   * rapid-fire the beacon. Kind is the broad bucket the backend
   * accepts; native `<video>` errors map to "unknown".
   */
  onFatalError?: (
    kind: "manifest" | "network" | "media" | "timeout" | "unknown",
    details?: string,
  ) => void;
}

interface UseLiveHlsReturn {
  error: string | null;
  loading: boolean;
  reload: () => void;
}

export function useLiveHls({
  videoRef,
  streamUrl,
  unavailableMessage,
  timeoutMs = 20_000,
  onFirstPlay,
  onFatalError,
}: UseLiveHlsOptions): UseLiveHlsReturn {
  const hlsRef = useRef<Hls | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

  // Keep onFirstPlay reference stable inside the effect without
  // forcing the effect to re-run on every render (a fresh closure
  // from the caller would otherwise tear down the HLS instance and
  // fire the beacon again).
  const onFirstPlayRef = useRef(onFirstPlay);
  onFirstPlayRef.current = onFirstPlay;
  const onFatalErrorRef = useRef(onFatalError);
  onFatalErrorRef.current = onFatalError;

  const reload = useCallback(() => setReloadToken((n) => n + 1), []);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !streamUrl) return;

    setError(null);
    setLoading(true);

    // Tear down any previous instance before attaching a new one.
    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }
    video.removeAttribute("src");
    video.load();

    let playing = false;
    let beaconFired = false;
    let fatalReported = false;
    const reportFatal = (
      kind: "manifest" | "network" | "media" | "timeout" | "unknown",
      details?: string,
    ) => {
      if (fatalReported) return;
      fatalReported = true;
      try {
        onFatalErrorRef.current?.(kind, details);
      } catch {
        // Beacon must never break playback fallback.
      }
    };
    const unavailableTimer = window.setTimeout(() => {
      if (!playing) {
        setError(unavailableMessage);
        reportFatal("timeout", `no first frame in ${timeoutMs}ms`);
      }
    }, timeoutMs);
    const onPlaying = () => {
      playing = true;
      setLoading(false);
      window.clearTimeout(unavailableTimer);
      if (!beaconFired) {
        beaconFired = true;
        onFirstPlayRef.current?.();
      }
    };
    video.addEventListener("playing", onPlaying);

    const startNative = () => {
      video.src = streamUrl;
      video.load();
      video.play().catch(() => {});
      const onErr = () => {
        setError(unavailableMessage);
        reportFatal("unknown", "native <video> error after hls.js fallback");
      };
      video.addEventListener("error", onErr, { once: true });
    };

    if (Hls.isSupported()) {
      // Buffering values tuned for live IPTV transmux (segments are
      // 2 s after the backend tuning). Larger numbers trade live
      // latency for resilience to upstream jitter — Xtream providers
      // often deliver MPEG-TS in uneven bursts, and the player has
      // to ride that out without falling behind the manifest window.
      //   maxBufferLength=60 / maxMaxBufferLength=120: pre-load up to
      //     2 minutes ahead so a 10-second upstream blip doesn't
      //     drain the buffer.
      //   liveSyncDurationCount=4 (× 2 s segments = 8 s behind edge):
      //     a touch more lag than the hls.js default (3) for steadier
      //     playback against jittery upstreams.
      //   liveMaxLatencyDurationCount=12 (× 2 s = 24 s): twice as
      //     much room before hls.js force-jumps to the live edge,
      //     which is what the user sees as a visible "skip".
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        maxBufferLength: 60,
        maxMaxBufferLength: 120,
        backBufferLength: 30,
        liveSyncDurationCount: 4,
        liveMaxLatencyDurationCount: 12,
        manifestLoadingMaxRetry: 6,
        manifestLoadingRetryDelay: 1000,
        manifestLoadingMaxRetryTimeout: 8000,
        levelLoadingMaxRetry: 6,
        levelLoadingRetryDelay: 1000,
        levelLoadingMaxRetryTimeout: 8000,
        fragLoadingMaxRetry: 6,
        fragLoadingRetryDelay: 1000,
        fragLoadingMaxRetryTimeout: 8000,
        xhrSetup: (xhr) => {
          // Auth via HTTP-only cookies (same-origin).
          xhr.withCredentials = true;
        },
      });
      hlsRef.current = hls;
      hls.loadSource(streamUrl);
      hls.attachMedia(video);

      let networkRetries = 0;
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.play().catch(() => {});
      });
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal) return;
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          hls.recoverMediaError();
          return;
        }
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          if (networkRetries < 3) {
            networkRetries++;
            hls.startLoad();
            return;
          }
        }
        // Map hls.js fatal type → server kind. We classify before
        // tearing down so the beacon goes out before native fallback
        // potentially overwrites the failure with a different one.
        // MEDIA_ERROR is handled with recoverMediaError() above, so by
        // the time we reach this point data.type is NETWORK_ERROR or one
        // of the OTHER_ERROR variants — no live media branch to map.
        const kind: "manifest" | "network" | "unknown" =
          data.type === Hls.ErrorTypes.NETWORK_ERROR
            ? data.details === Hls.ErrorDetails.MANIFEST_LOAD_ERROR ||
              data.details === Hls.ErrorDetails.MANIFEST_LOAD_TIMEOUT ||
              data.details === Hls.ErrorDetails.MANIFEST_PARSING_ERROR
              ? "manifest"
              : "network"
            : "unknown";
        reportFatal(kind, String(data.details ?? data.type ?? "fatal"));
        hls.destroy();
        hlsRef.current = null;
        startNative();
      });
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = streamUrl;
      video.load();
      video.play().catch(() => {});
      const onErr = () => {
        setError(unavailableMessage);
        reportFatal("unknown", "native Safari HLS error");
      };
      video.addEventListener("error", onErr, { once: true });
    } else {
      startNative();
    }

    return () => {
      window.clearTimeout(unavailableTimer);
      video.removeEventListener("playing", onPlaying);
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
    };
  }, [videoRef, streamUrl, unavailableMessage, timeoutMs, reloadToken]);

  return { error, loading, reload };
}
