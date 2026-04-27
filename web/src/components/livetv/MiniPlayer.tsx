import { useTranslation } from "react-i18next";
import { useLiveTvPlayer } from "@/store/liveTvPlayer";
import { ChannelPlayer } from "./ChannelPlayer";

/**
 * MiniPlayer — corner-pinned live-TV pip that survives navigation.
 *
 * Mounted at the AppLayout level (NOT inside a route) so the user
 * can wander between /movies, /series, /search, etc. while the
 * channel keeps streaming. Click the body to re-expand to the full
 * overlay; the X button stops playback completely.
 *
 * The mini renders the same <ChannelPlayer> as the overlay does — but
 * because they're separate React subtrees, switching between mini and
 * overlay is technically a remount of the underlying <video>. We
 * accept the brief reload (≤ 1 s) over the architectural complexity
 * of portaling a single <video> across two trees with hls.js
 * mid-flight; the alternative — promoting the player into a global
 * portal — adds far more state-coordination risk than it saves UX.
 *
 * Skips rendering when:
 *   - Nothing is playing (`channel == null`).
 *   - The full overlay is up (`expanded == true`) — that surface
 *     owns the player at that moment.
 */
export function MiniPlayer() {
  const { t } = useTranslation();
  const channel = useLiveTvPlayer((s) => s.channel);
  const expanded = useLiveTvPlayer((s) => s.expanded);
  const expand = useLiveTvPlayer((s) => s.expand);
  const stop = useLiveTvPlayer((s) => s.stop);

  if (!channel || expanded) return null;

  return (
    <aside
      data-theme="tv"
      data-accent="lime"
      className="fixed bottom-4 right-4 z-50 flex w-[360px] max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-tv-lg border border-tv-line bg-tv-bg-0 shadow-tv-lg motion-safe:animate-fade-in"
      aria-label={t("liveTV.miniPlayer", { defaultValue: "Reproductor flotante" })}
    >
      {/* Click the video area to expand — the entire 16:9 surface is a
          giant button so the affordance is obvious without a separate
          "expand" icon competing with the close button. */}
      <button
        type="button"
        onClick={expand}
        className="group relative aspect-video w-full overflow-hidden bg-black focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        aria-label={t("liveTV.miniExpand", {
          defaultValue: "Expandir reproductor",
        })}
      >
        <ChannelPlayer channel={channel} />

        {/* Hover scrim hints "click to expand" without a heavy icon
            dominating the small surface. Auto-revealed on hover/focus
            so a passive user doesn't see the chrome. */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition-opacity group-hover:bg-black/30 group-hover:opacity-100 group-focus-visible:bg-black/30 group-focus-visible:opacity-100">
          <span className="rounded-full bg-black/60 px-3 py-1 text-xs font-medium text-white">
            {t("liveTV.miniExpand", { defaultValue: "Expandir" })}
          </span>
        </div>
      </button>

      <div className="flex items-center gap-2 border-t border-tv-line bg-tv-bg-1 px-3 py-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-semibold text-tv-fg-0">
            {channel.name}
          </div>
          <div className="truncate font-mono text-[10px] uppercase tracking-wider text-tv-fg-3">
            CH {channel.number}
          </div>
        </div>
        <button
          type="button"
          onClick={stop}
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-tv-fg-2 transition-colors hover:bg-tv-bg-2 hover:text-tv-fg-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          aria-label={t("liveTV.miniStop", {
            defaultValue: "Detener reproductor",
          })}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>
    </aside>
  );
}
