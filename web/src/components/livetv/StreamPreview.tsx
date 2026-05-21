import { useEffect, useRef } from "react";
import Hls from "hls.js";

interface StreamPreviewProps {
  streamUrl: string;
  /**
   * CSS classes for the `<video>` element. Defaults to filling the
   * parent. Pass a custom class for non-default layouts (cover vs
   * contain, custom sizing).
   */
  className?: string;
}

/**
 * StreamPreview — mounts a muted HLS video over whatever the caller
 * sizes it to. Shared between ChannelCard's hover preview and the
 * Hero spotlight's auto-preview.
 *
 * Kept lean on purpose: no loading UI, no error UI, silent failure.
 * Network retries are aggressive-short (1 each) because a preview that
 * struggles for several seconds is worse than no preview at all.
 *
 * `pointer-events-none` on the `<video>` lets mouse events fall
 * through to the parent — important for ChannelCard where the button
 * needs to keep receiving hover, click, and focus signals while the
 * preview is visible.
 */
export function StreamPreview({
  streamUrl,
  className = "absolute inset-0 size-full object-cover",
}: StreamPreviewProps) {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    let hls: Hls | null = null;
    const onManifestParsed = () => {
      video.play().catch(() => {});
    };
    const onHlsError = (_event: unknown, data: { fatal: boolean }) => {
      if (data.fatal) hls?.destroy();
    };

    if (Hls.isSupported()) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        maxBufferLength: 8,
        maxMaxBufferLength: 10,
        backBufferLength: 0,
        manifestLoadingMaxRetry: 1,
        levelLoadingMaxRetry: 1,
        fragLoadingMaxRetry: 1,
        xhrSetup: (xhr) => {
          xhr.withCredentials = true;
        },
      });
      hls.loadSource(streamUrl);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, onManifestParsed);
      hls.on(Hls.Events.ERROR, onHlsError);
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = streamUrl;
      video.play().catch(() => {});
    }

    return () => {
      if (hls) {
        hls.off(Hls.Events.MANIFEST_PARSED, onManifestParsed);
        hls.off(Hls.Events.ERROR, onHlsError);
        hls.destroy();
      }
      video.removeAttribute("src");
      video.load();
    };
  }, [streamUrl]);

  return (
    <video
      ref={videoRef}
      muted
      playsInline
      autoPlay
      className={`pointer-events-none ${className}`}
    />
  );
}
