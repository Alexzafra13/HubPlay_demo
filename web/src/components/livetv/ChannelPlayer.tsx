import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import { useRecordChannelWatch } from "@/api/hooks";
import type { Channel } from "@/api/types";
import { Spinner } from "@/components/common";
import { useLiveHls } from "@/hooks/useLiveHls";

interface ChannelPlayerProps {
  channel: Channel;
}

export function ChannelPlayer({ channel }: ChannelPlayerProps) {
  const { t } = useTranslation();
  const videoRef = useRef<HTMLVideoElement>(null);
  const recordWatch = useRecordChannelWatch();

  // Beacon: fired exactly once per streamUrl when the first frame
  // actually plays (useLiveHls guarantees first-play-only — see
  // onFirstPlay contract). Failures swallowed: the rail simply won't
  // update on this view. The channel id is captured in the closure
  // at render time so re-renders against a new channel get a fresh
  // beacon automatically; useLiveHls tears down + re-attaches on
  // streamUrl change and re-arms its beacon flag.
  const channelId = channel.id;

  // PB-28: id de viewer efímero por montaje. Viaja como `?v=` en la
  // URL del stream (el 302 al transmux lo propaga y el manifest
  // registra al viewer); al zapear/desmontar, la baja explícita libera
  // el slot de ffmpeg al instante en vez de esperar al idle reap de
  // 30s — visitar >MaxSessions canales en <30s daba TRANSMUX_BUSY.
  // Id efímero congelado por montaje vía useState-initializer (los
  // refs no pueden leerse en render bajo el React Compiler). Debe
  // existir en el PRIMER render — la URL del stream lo lleva — así
  // que generarlo en un effect re-montaría el stream entero.
  const [viewerId] = useState<string>(
    () => globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`,
  );
  const streamUrl = useMemo(() => {
    const sep = channel.stream_url.includes("?") ? "&" : "?";
    return `${channel.stream_url}${sep}v=${encodeURIComponent(viewerId)}`;
  }, [channel.stream_url, viewerId]);

  useEffect(() => {
    const leave = () => {
      api.leaveChannelStream(channelId, viewerId).catch(() => {
        // Best-effort: el idle reap del server es el backstop.
      });
    };
    // pagehide cubre cierre de pestaña/navegación dura; el cleanup del
    // effect cubre el zapping dentro de la SPA (cambia channelId).
    window.addEventListener("pagehide", leave);
    return () => {
      window.removeEventListener("pagehide", leave);
      leave();
    };
  }, [channelId, viewerId]);

  const onFirstPlay = useCallback(() => {
    recordWatch.mutate(channelId, {
      onError: (err) => {
        // Non-fatal — log for devtools visibility, nothing else.
        console.warn("[continue-watching] beacon failed:", err);
      },
    });
  }, [channelId, recordWatch]);

  // Fatal-error beacon: fired at most once per stream attachment from
  // useLiveHls when hls.js gives up. Forwards the failure into the
  // channel-health pipeline so client-side dead-stream signal joins
  // the same `consecutive_failures` counter the proxy already drives.
  // Failures here are non-fatal — the player UI already shows the
  // user the stream failed; the beacon is pure telemetry.
  const onFatalError = useCallback(
    (
      kind: "manifest" | "network" | "media" | "timeout" | "unknown",
      details?: string,
    ) => {
      api.reportPlaybackFailure(channelId, kind, details).catch((err) => {
        console.warn("[playback-failure] beacon failed:", err);
      });
    },
    [channelId],
  );

  const { error, loading, reload } = useLiveHls({
    videoRef,
    streamUrl,
    unavailableMessage: t("liveTV.channelUnavailable"),
    onFirstPlay,
    onFatalError,
  });

  return (
    <div className="relative size-full bg-black">
      {loading && !error && (
        <div className="absolute inset-0 flex items-center justify-center z-10">
          <Spinner size="lg" />
        </div>
      )}
      {error && (
        <div className="absolute inset-0 flex items-center justify-center z-10 bg-black/60">
          <div className="text-center px-4">
            <svg
              width="40"
              height="40"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1"
              className="mx-auto mb-3 text-text-muted/40"
            >
              <rect x="2" y="4" width="20" height="14" rx="2" />
              <path d="M7 22h10M12 18v4" />
              <path
                d="M8 11l8 0M8 11l2-2M8 11l2 2"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            <p className="text-sm text-text-muted mb-3">{error}</p>
            <button
              type="button"
              onClick={reload}
              className="px-5 py-2 rounded-lg bg-accent/20 text-sm font-medium text-accent hover:bg-accent/30 transition-all"
            >
              {t("common.retry")}
            </button>
          </div>
        </div>
      )}
      <video
        ref={videoRef}
        controls
        className="size-full object-contain"
        playsInline
        aria-label={channel.name}
      />
    </div>
  );
}
