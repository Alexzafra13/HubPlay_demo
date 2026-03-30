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

  // Save final progress on unmount
  useEffect(() => {
    return () => {
      const video = videoRef.current;
      if (video && video.currentTime > 0) {
        api
          .updateProgress(itemId, {
            position_ticks: Math.floor(video.currentTime * TICKS_PER_SECOND),
          })
          .catch(() => {});
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}
