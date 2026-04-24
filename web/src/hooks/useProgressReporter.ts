import { useEffect, useRef } from "react";
import type { RefObject } from "react";
import { api } from "@/api/client";

const PROGRESS_SAVE_INTERVAL = 10_000;
const TICKS_PER_SECOND = 10_000_000;

export function useProgressReporter(
  videoRef: RefObject<HTMLVideoElement | null>,
  itemId: string,
): void {
  const progressTimerRef = useRef<ReturnType<typeof setInterval>>(0 as never);

  // Periodic progress save
  useEffect(() => {
    progressTimerRef.current = setInterval(() => {
      const video = videoRef.current;
      if (video && !video.paused && video.currentTime > 0) {
        api
          .updateProgress(itemId, {
            position_ticks: Math.floor(video.currentTime * TICKS_PER_SECOND),
          })
          .catch(() => {});
      }
    }, PROGRESS_SAVE_INTERVAL);

    return () => clearInterval(progressTimerRef.current);
  }, [videoRef, itemId]);

  // Save final progress on unmount. videoRef.current must be captured at
  // effect-mount time (per react-hooks/exhaustive-deps) — by the time the
  // cleanup runs, React may have already nulled the ref. Since the parent
  // only creates the <video> once and passes the same ref for the
  // component's lifetime, capturing at mount gives us the correct node.
  useEffect(() => {
    const video = videoRef.current;
    return () => {
      if (video && video.currentTime > 0) {
        api
          .updateProgress(itemId, {
            position_ticks: Math.floor(video.currentTime * TICKS_PER_SECOND),
          })
          .catch(() => {});
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- unmount-only; itemId is stable for the player's life
  }, []);
}
