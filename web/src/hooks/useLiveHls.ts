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
}: UseLiveHlsOptions): UseLiveHlsReturn {
  const hlsRef = useRef<Hls | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

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
    const unavailableTimer = window.setTimeout(() => {
      if (!playing) setError(unavailableMessage);
    }, timeoutMs);
    const onPlaying = () => {
      playing = true;
      setLoading(false);
      window.clearTimeout(unavailableTimer);
    };
    video.addEventListener("playing", onPlaying);

    const startNative = () => {
      video.src = streamUrl;
      video.load();
      video.play().catch(() => {});
      const onErr = () => setError(unavailableMessage);
      video.addEventListener("error", onErr, { once: true });
    };

    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        backBufferLength: 30,
        liveSyncDurationCount: 3,
        liveMaxLatencyDurationCount: 6,
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
        hls.destroy();
        hlsRef.current = null;
        startNative();
      });
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = streamUrl;
      video.load();
      video.play().catch(() => {});
      const onErr = () => setError(unavailableMessage);
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
