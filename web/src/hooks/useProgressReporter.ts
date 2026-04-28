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
      if (video && video.currentTime > 0) {
        api
          .updateProgress(
            itemId,
            {
              position_ticks: Math.floor(video.currentTime * TICKS_PER_SECOND),
            },
            { keepalive: true },
          )
          .catch(() => {});
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- unmount-only; itemId is stable for the player's life
  }, []);
}
