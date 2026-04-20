import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { formatTime, getProgramProgress } from "./epgHelpers";

export interface HeroTileData {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
}

interface HeroMosaicProps {
  /**
   * Ordered featured tiles. The first entry becomes the large "main" tile;
   * up to four more fill the side grid. Extras are ignored, so callers
   * can pass a larger list without trimming.
   */
  items: HeroTileData[];
  onOpen?: (channel: Channel) => void;
}

/**
 * HeroMosaic — the top-of-page showcase for the "Discover" view.
 *
 * Layout: a 12-column grid where the main tile occupies 7 columns and
 * 2 rows; four side tiles fill the remaining 5 columns as a 2×2 grid.
 * On narrow viewports the mosaic collapses to a single column and only
 * the main tile stays above the fold, keeping the important surface
 * first even on mobile.
 */
export function HeroMosaic({ items, onOpen }: HeroMosaicProps) {
  if (items.length === 0) return null;
  const [main, ...rest] = items;
  const sides = rest.slice(0, 4);

  // Mirrors the /diseño/ prototype: a 3-column grid (1.4fr / 1fr / 1fr) with
  // the main tile pinned to col 1 across both rows, and up to 4 side tiles
  // flowing into the 2×2 block on the right.
  //
  // Earlier version used `grid-cols-12` with `col-span-5` sides, which only
  // fit 2 sides per 2-row layout and spilled the rest into phantom rows 3-4
  // (leaving a huge empty gradient under the main tile and pushing the
  // rails below the fold).
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-[1.4fr_1fr_1fr] md:grid-rows-2">
      <HeroTile
        data={main}
        variant="main"
        onOpen={onOpen}
        className="md:col-start-1 md:row-span-2"
      />
      {sides.map((item) => (
        <HeroTile
          key={item.channel.id}
          data={item}
          variant="side"
          onOpen={onOpen}
        />
      ))}
    </div>
  );
}

interface HeroTileProps {
  data: HeroTileData;
  variant: "main" | "side";
  onOpen?: (channel: Channel) => void;
  className?: string;
}

function HeroTile({ data, variant, onOpen, className = "" }: HeroTileProps) {
  const { channel, nowPlaying } = data;
  const isMain = variant === "main";
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  // Gradient derived from the channel's deterministic logo color — gives
  // every tile a distinct visual identity without needing real artwork.
  const bg = `linear-gradient(135deg, ${channel.logo_bg} 0%, var(--tv-bg-1) 120%)`;

  return (
    <button
      type="button"
      onClick={() => onOpen?.(channel)}
      className={[
        "group relative overflow-hidden rounded-tv-lg border border-tv-line text-left transition hover:border-tv-line-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent",
        // Heights tuned so the whole mosaic + chips + first rail stay
        // above the fold on a 900 px laptop viewport. Main tile stays
        // taller than sides to keep the "hero" feel.
        isMain ? "min-h-[220px] md:min-h-[280px]" : "min-h-[130px]",
        className,
      ].join(" ")}
      aria-label={
        nowPlaying ? `${channel.name} — ${nowPlaying.title}` : channel.name
      }
    >
      <div className="absolute inset-0" style={{ background: bg }} />
      <div
        className="absolute inset-0 bg-gradient-to-t from-black/75 via-black/20 to-transparent"
        aria-hidden="true"
      />

      {/* Top meta row — LIVE only shows when we actually have EPG for
          "now on air"; otherwise just the category tag is enough. */}
      <div className="absolute left-4 right-4 top-4 flex items-center gap-2">
        {nowPlaying && (
          <span className="flex items-center gap-1 rounded-tv-xs bg-tv-live/90 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wider text-white">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
            Live
          </span>
        )}
        <span className="rounded-tv-xs bg-black/40 px-2 py-0.5 text-[11px] font-medium text-tv-fg-0 backdrop-blur">
          {channel.category.charAt(0).toUpperCase() +
            channel.category.slice(1)}
        </span>
        {channel.country && (
          <span className="rounded-tv-xs bg-black/40 px-2 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-tv-fg-1 backdrop-blur">
            {channel.country}
          </span>
        )}
      </div>

      {/* Bottom content */}
      <div className="absolute inset-x-4 bottom-4 flex flex-col gap-3">
        <div className="flex items-end gap-3">
          <ChannelLogo
            logoUrl={channel.logo_url}
            initials={channel.logo_initials}
            bg={channel.logo_bg}
            fg={channel.logo_fg}
            name={channel.name}
            className={
              isMain
                ? "h-14 w-14 rounded-tv-md ring-2 ring-white/10"
                : "h-10 w-10 rounded-tv-sm ring-2 ring-white/10"
            }
            textClassName={isMain ? "text-base font-bold" : "text-xs font-bold"}
          />
          <div className="min-w-0 flex-1">
            <div className="truncate font-mono text-[10px] uppercase tracking-widest text-tv-fg-2">
              CH {channel.number}
            </div>
            <div
              className={[
                "truncate font-semibold text-tv-fg-0",
                isMain ? "text-xl md:text-2xl" : "text-sm",
              ].join(" ")}
            >
              {channel.name}
            </div>
          </div>
        </div>

        {nowPlaying && (
          <>
            <div
              className={[
                "line-clamp-2 text-tv-fg-1",
                isMain ? "text-base" : "text-xs",
              ].join(" ")}
            >
              {nowPlaying.title}
            </div>
            <div className="flex items-center gap-2">
              <div className="h-1 flex-1 overflow-hidden rounded-full bg-white/10">
                <div
                  className="h-full rounded-full bg-tv-accent"
                  style={{ width: `${progress}%` }}
                />
              </div>
              <span className="font-mono text-[10px] tabular-nums text-tv-fg-2">
                {formatTime(nowPlaying.end_time)}
              </span>
            </div>
          </>
        )}
      </div>
    </button>
  );
}
