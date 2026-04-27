import { create } from "zustand";
import type { Channel } from "@/api/types";

/**
 * LiveTvPlayerState — global state for the persistent live-TV player.
 *
 * Why global (vs. local to the LiveTV page)?
 *   The page-local state we had before lived inside <LiveTV>. As soon
 *   as the user navigated to /movies the page unmounted and the
 *   playing channel was lost. The product expectation for a "TV app"
 *   is that the stream survives navigation — collapse to a corner
 *   mini-player, audio keeps going, the user can wander around the app
 *   and come back. That requires the state to live above any single
 *   route, hence a Zustand store mounted at the app shell.
 *
 * The store deliberately does NOT own the actual <video> element or
 * HLS.js instance. Those live inside <ChannelPlayer> (the player
 * component) so the video element can move from the overlay to the
 * mini-player via React tree without the audio/video hiccup that a
 * remount would cause. The store just tracks WHICH channel and HOW
 * BIG (overlay vs. mini), and provides next/prev navigation across
 * the user's currently-visible channel list.
 *
 * `surfList` is the ordered channel list the user is browsing (e.g.
 * the current "Ahora" filtered grid, or "Favoritos"). When the user
 * presses ↑/↓ to surf, we walk this list. The page that opened the
 * player is responsible for setting it; we fall back to a single-item
 * list if the page didn't supply one.
 */
export interface LiveTvPlayerState {
  /** Channel currently in the player. `null` means nothing is playing. */
  channel: Channel | null;
  /** Whether the full overlay is up (true) or the corner mini is showing (false). */
  expanded: boolean;
  /** Ordered channel list for ↑/↓ surfing. Caller-supplied. */
  surfList: Channel[];

  /** Open the full overlay on `channel`, optionally seeding the surf list. */
  open: (channel: Channel, surfList?: Channel[]) => void;
  /**
   * Stop playback entirely. Use this for the explicit "X" / close on the
   * mini-player; the overlay's back button calls `collapse()` instead so
   * audio survives.
   */
  stop: () => void;
  /** Collapse the overlay to the corner mini-player. */
  collapse: () => void;
  /** Re-expand the mini-player to the full overlay. */
  expand: () => void;
  /** Switch to the next channel in `surfList`, wrapping around. */
  surfNext: () => void;
  /** Switch to the previous channel in `surfList`, wrapping around. */
  surfPrev: () => void;
  /** Replace the surf list (e.g. when the parent's filtered list changes). */
  setSurfList: (list: Channel[]) => void;
}

export const useLiveTvPlayer = create<LiveTvPlayerState>()((set, get) => ({
  channel: null,
  expanded: false,
  surfList: [],

  open(channel, surfList) {
    set({
      channel,
      expanded: true,
      surfList: surfList && surfList.length > 0 ? surfList : [channel],
    });
  },
  stop() {
    set({ channel: null, expanded: false, surfList: [] });
  },
  collapse() {
    set({ expanded: false });
  },
  expand() {
    set({ expanded: true });
  },
  surfNext() {
    const { channel, surfList } = get();
    if (!channel || surfList.length < 2) return;
    const i = surfList.findIndex((c) => c.id === channel.id);
    const next = surfList[(i + 1) % surfList.length];
    set({ channel: next });
  },
  surfPrev() {
    const { channel, surfList } = get();
    if (!channel || surfList.length < 2) return;
    const i = surfList.findIndex((c) => c.id === channel.id);
    const prev = surfList[(i - 1 + surfList.length) % surfList.length];
    set({ channel: prev });
  },
  setSurfList(list) {
    set({ surfList: list });
  },
}));
