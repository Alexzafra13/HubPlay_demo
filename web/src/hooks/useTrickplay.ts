import { useQuery } from "@tanstack/react-query";
import { queryKeys } from "@/api/queryKeys";

/**
 * The wire shape of `<api>/items/{id}/trickplay.json`. Mirrors
 * `imaging.TrickplayManifest` server-side so the math here is
 * identical to anyone reading the Go code:
 *
 *   thumbIdx = floor(seekSeconds / interval_sec)
 *   col      = thumbIdx % columns
 *   row      = thumbIdx / columns        (integer)
 *   x_px     = col * thumb_width
 *   y_px     = row * thumb_height
 *
 * The SeekBar applies that as a CSS background-position offset on a
 * div sized thumb_width × thumb_height, with the sprite as
 * background-image.
 */
export interface TrickplayManifest {
  interval_sec: number;
  thumb_width: number;
  thumb_height: number;
  columns: number;
  rows: number;
  total: number;
}

export interface TrickplayState {
  manifest: TrickplayManifest | null;
  /** URL to the sprite PNG; same-origin cookies handle auth. */
  spriteURL: string;
  /** False when the backend returned 503 (provider disabled) or any
   *  other error — the SeekBar should silently render without a
   *  preview tooltip rather than show a broken affordance. */
  available: boolean;
}

const UNAVAILABLE: TrickplayState = {
  manifest: null,
  spriteURL: "",
  available: false,
};

async function fetchTrickplay(itemId: string): Promise<TrickplayManifest> {
  const url = `/api/v1/items/${encodeURIComponent(itemId)}/trickplay.json`;
  const r = await fetch(url, { credentials: "same-origin" });
  if (!r.ok) throw new Error(`status ${r.status}`);
  // The trickplay endpoint is one of the few that serves the
  // manifest without the standard `{data: ...}` envelope (it
  // ServeFile's the JSON straight from disk). Tolerate either
  // shape so the contract can tighten without breaking the UI.
  const body = (await r.json()) as { data: TrickplayManifest } | TrickplayManifest;
  return "data" in body ? body.data : body;
}

/**
 * useTrickplay fetches the trickplay manifest for an item. The first
 * hit on the server triggers ffmpeg generation (5-30 s) so we accept
 * whatever latency that takes; the player works fine without
 * trickplay during that window — the SeekBar just doesn't show a
 * preview until `available` flips true.
 *
 * Wraps TanStack Query so a second mount of the same item (e.g.
 * navigating away and back, or re-opening the player) reuses the
 * cached manifest instead of re-paying the cold-start latency. The
 * sprite PNG itself is HTTP-cached by the browser at the network
 * layer, but the JSON manifest needs the in-memory cache.
 *
 * `itemId === ""` is a no-op (no fetch). Used by VideoPlayer to gate
 * the hook on "we actually have an item to preview".
 */
export function useTrickplay(itemId: string): TrickplayState {
  const { data, isError } = useQuery({
    queryKey: queryKeys.trickplay(itemId),
    queryFn: () => fetchTrickplay(itemId),
    enabled: itemId !== "",
    // Manifest content is immutable for the lifetime of an item — once
    // ffmpeg generates the sprite the contract is fixed. 5-minute
    // staleness covers a session of bouncing between items without
    // re-paying the round trip.
    staleTime: 5 * 60_000,
    // 503 (TRICKPLAY_DISABLED) and network errors land in error state.
    // Both are non-fatal: the SeekBar falls back to no preview, and
    // retrying buys nothing — provider state isn't going to change
    // mid-session.
    retry: false,
  });

  if (!itemId || isError || !data) {
    return UNAVAILABLE;
  }
  return {
    manifest: data,
    spriteURL: `/api/v1/items/${encodeURIComponent(itemId)}/trickplay.png`,
    available: true,
  };
}
