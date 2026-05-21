import { useEffect, useRef } from "react";
import type { RefObject } from "react";
import { api } from "@/api/client";

const PROGRESS_SAVE_INTERVAL = 10_000;
const TICKS_PER_SECOND = 10_000_000;

// useProgressReporter periodically saves the player's position and
// fires one final save on unmount. Pass `peerId` when playing a
// federated item -- progress routes through the federation_progress
// table on the LOCAL server (the origin peer never learns who is
// watching, only that something is being streamed).
export function useProgressReporter(
  videoRef: RefObject<HTMLVideoElement | null>,
  itemId: string,
  peerId?: string,
): void {
  const progressTimerRef = useRef<ReturnType<typeof setInterval>>(0 as never);

  // Periodic progress save
  useEffect(() => {
    progressTimerRef.current = setInterval(() => {
      const video = videoRef.current;
      if (!video || video.paused || video.currentTime <= 0) return;
      // Don't write while a seek is mid-flight: video.currentTime is
      // briefly the pre-seek sample (the new buffer hasn't landed
      // yet) and persisting that as "where the user is" would corrupt
      // resume on the next session — they'd come back to the OLD
      // position even though they had clearly seeked away.
      if (video.seeking) return;
      const positionTicks = Math.floor(video.currentTime * TICKS_PER_SECOND);
      const durationTicks =
        Number.isFinite(video.duration) && video.duration > 0
          ? Math.floor(video.duration * TICKS_PER_SECOND)
          : 0;
      if (peerId) {
        api
          .updatePeerItemProgress(peerId, itemId, {
            position_ticks: positionTicks,
            duration_ticks: durationTicks,
          })
          .catch(() => {});
      } else {
        api.updateProgress(itemId, { position_ticks: positionTicks }).catch(() => {});
      }
    }, PROGRESS_SAVE_INTERVAL);

    return () => clearInterval(progressTimerRef.current);
  }, [videoRef, itemId, peerId]);

  // Save final progress on unmount. videoRef.current must be captured at
  // effect-mount time (per react-hooks/exhaustive-deps) — by the time the
  // cleanup runs, React may have already nulled the ref. Since the parent
  // only creates the <video> once and passes the same ref for the
  // component's lifetime, capturing at mount gives us the correct node.
  //
  // `keepalive: true` is what makes this race-proof. Without it, the user
  // closing the tab (or React unmounting because of navigation) aborts
  // the in-flight fetch and the final position is lost. With it, the
  // browser commits the request to the network stack and lets it ride
  // out independently of the page's lifecycle. Payload is < 200 bytes,
  // well under the 64 KiB keepalive cap.
  useEffect(() => {
    const video = videoRef.current;
    return () => {
      if (!video || video.currentTime <= 0) return;
      // Same rationale as the periodic save above: don't persist a
      // mid-seek position. If the unmount races with a seek the user
      // just kicked off, write the LAST known good (paused or settled)
      // sample only.
      if (video.seeking) return;
      const positionTicks = Math.floor(video.currentTime * TICKS_PER_SECOND);
      const durationTicks =
        Number.isFinite(video.duration) && video.duration > 0
          ? Math.floor(video.duration * TICKS_PER_SECOND)
          : 0;
      if (peerId) {
        api
          .updatePeerItemProgress(
            peerId,
            itemId,
            { position_ticks: positionTicks, duration_ticks: durationTicks },
            { keepalive: true },
          )
          .catch(() => {});
      } else {
        api
          .updateProgress(
            itemId,
            { position_ticks: positionTicks },
            { keepalive: true },
          )
          .catch(() => {});
      }
    };
    // Deps included properly: if itemId/peerId change mid-flight (rare
    // — the parent doesn't recycle the player across items), the cleanup
    // persists the OLD progress before the new effect captures fresh
    // refs. Same end result as the previous empty-deps version, and
    // satisfies the React Compiler's "all rules of React" requirement.
  }, [videoRef, itemId, peerId]);
}
