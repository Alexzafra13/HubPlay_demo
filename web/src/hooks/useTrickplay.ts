import { useEffect, useState } from "react";

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

/**
 * useTrickplay fetches the trickplay manifest for an item once on
 * mount. The first hit on the server triggers ffmpeg generation
 * (5-30 s) so we accept whatever latency that takes; the player
 * works fine without trickplay during that window — the SeekBar
 * just doesn't show a preview until `available` flips true.
 *
 * `itemId === ""` is a no-op (no fetch). Used by VideoPlayer to gate
 * the hook on "we actually have an item to preview".
 */
export function useTrickplay(itemId: string): TrickplayState {
  const [state, setState] = useState<TrickplayState>({
    manifest: null,
    spriteURL: "",
    available: false,
  });

  useEffect(() => {
    if (!itemId) {
      setState({ manifest: null, spriteURL: "", available: false });
      return;
    }
    const ctl = new AbortController();
    const url = `/api/v1/items/${encodeURIComponent(itemId)}/trickplay.json`;
    fetch(url, { credentials: "same-origin", signal: ctl.signal })
      .then((r) => {
        if (!r.ok) throw new Error(`status ${r.status}`);
        return r.json() as Promise<{ data: TrickplayManifest } | TrickplayManifest>;
      })
      .then((body) => {
        // The trickplay endpoint is one of the few that serves the
        // manifest without the standard `{data: ...}` envelope (it
        // ServeFile's the JSON straight from disk). Tolerate either
        // shape so the contract can tighten without breaking the UI.
        const manifest = "data" in body ? body.data : body;
        setState({
          manifest,
          spriteURL: `/api/v1/items/${encodeURIComponent(itemId)}/trickplay.png`,
          available: true,
        });
      })
      .catch(() => {
        // 503 (TRICKPLAY_DISABLED) and network errors land here.
        // Both are non-fatal: the SeekBar falls back to no preview.
        setState({ manifest: null, spriteURL: "", available: false });
      });
    return () => ctl.abort();
  }, [itemId]);

  return state;
}
