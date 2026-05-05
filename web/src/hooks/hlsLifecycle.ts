// Shared HLS lifecycle utilities for the VOD (`useHls`) and live
// (`useLiveHls`) player hooks. Both hooks open and tear down hls.js
// instances against an HTMLVideoElement, and the boilerplate for
// "destroy the previous engine + reset the <video> src" was copied
// across both files — meaning a fix to one (e.g. the
// re-attach-on-source-change patch a061267) had to be ported by hand
// to the other or the players drifted out of sync silently.
//
// Centralising the operations here means a future tweak only happens
// once.

import type Hls from "hls.js";

/** Mutable ref shape used by both player hooks. */
export interface HlsRef {
  current: Hls | null;
}

/**
 * destroyHlsInstance — tear down any in-flight hls.js engine and
 * clear the bound <video>'s `src` so a transition (e.g. from
 * direct-play progressive URL to a fresh transcode HLS) does not
 * leave the previous source attached.
 *
 * Idempotent: safe to call when nothing is attached. Both player
 * hooks invoke it (1) up-front before attaching a new engine, and
 * (2) from the effect cleanup — strict-mode double-mount and React
 * 18 effect replay both rely on this being a no-op when there's
 * nothing to clean up.
 */
export function destroyHlsInstance(ref: HlsRef, video: HTMLVideoElement | null): void {
  if (ref.current) {
    ref.current.destroy();
    ref.current = null;
  }
  if (video) {
    // removeAttribute + load() is the documented way to drop a
    // <video>'s current source. Setting src = "" alone causes
    // Chrome/Firefox to keep the previous track buffered.
    video.removeAttribute("src");
    video.load();
  }
}
