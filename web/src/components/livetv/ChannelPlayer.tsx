import { useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
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
  const onFirstPlay = useCallback(() => {
    recordWatch.mutate(channelId, {
      onError: (err) => {
        // Non-fatal — log for devtools visibility, nothing else.
        console.warn("[continue-watching] beacon failed:", err);
      },
    });
  }, [channelId, recordWatch]);

  const { error, loading, reload } = useLiveHls({
    videoRef,
    streamUrl: channel.stream_url,
    unavailableMessage: t("liveTV.channelUnavailable"),
    onFirstPlay,
  });

  return (
    <div className="relative h-full w-full bg-black">
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
        className="h-full w-full object-contain"
        playsInline
        aria-label={channel.name}
      />
    </div>
  );
}
