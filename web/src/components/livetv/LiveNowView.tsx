import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { useDebounce } from "@/hooks/useDebounce";
import { usePagedItems } from "@/hooks/usePagedItems";
import { CategoryChips, type CategoryFilter } from "./CategoryChips";
import { ChannelCard } from "./ChannelCard";
import { getNowPlaying, getUpNext } from "./epgHelpers";

/**
 * Sort strategies for the "Ahora" mosaic. "ending" is the senior
 * favourite — it surfaces what's already wrapping up so the user can
 * either jump on the tail or skip to a fresher option, which is the
 * exact decision they came to the page to make.
 */
export type LiveNowSort = "favorites" | "ending" | "starting" | "name";

interface LiveNowViewProps {
  /** Channels currently broadcasting an EPG-confirmed programme. */
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  category: CategoryFilter;
  onCategoryChange: (c: CategoryFilter) => void;
  /** Counts scoped to currently-live channels — *not* the full lineup —
   * so a chip's number reflects "how many of these are on right now",
   * which is the only useful answer for this surface. */
  counts: Record<CategoryFilter, number>;
  search: string;
  sort: LiveNowSort;
  onSortChange: (s: LiveNowSort) => void;
  onOpen: (ch: Channel) => void;
  favoriteSet: Set<string>;
  onToggleFavorite: (channelId: string) => void;
}

/**
 * LiveNowView — the default landing surface on /live-tv.
 *
 * Answers the question the user actually asked when they opened the
 * page: "what do I put on right now?" — by showing every channel with
 * a programme currently broadcasting in a flat top-to-bottom grid.
 * Modeled on FuboTV / YouTube TV's "On now"; no hero card to pick a
 * single channel for them, no editorial framing — just the lineup.
 *
 * Filtering:
 *   - Category chips narrow by canonical category (Informativos /
 *     Deportes / etc.) with counts scoped to currently-live channels.
 *   - Search (from the global TopBar slot) cuts across channel name,
 *     group name, and the title of the programme on air right now —
 *     so a search for "telediario" finds the channel airing it even
 *     if the channel name doesn't contain that string.
 *
 * What it intentionally is NOT:
 *   - A schedule grid: the "Guía" tab covers that.
 *   - A library browser: the "Descubrir" tab covers exhaustive lineup
 *     exploration with rails per category and the editorial hero.
 */
export function LiveNowView({
  channels,
  scheduleByChannel,
  category,
  onCategoryChange,
  counts,
  search,
  sort,
  onSortChange,
  onOpen,
  favoriteSet,
  onToggleFavorite,
}: LiveNowViewProps) {
  const { t } = useTranslation();

  // Search is debounced so a 22k-channel library doesn't re-run the
  // filter+sort cascade on every keystroke. 200 ms is short enough to
  // feel instant while batching a typing burst into a single recompute.
  const debouncedSearch = useDebounce(search, 200);

  // Filter (category + search) and then sort according to the user's
  // strategy. Done in a single useMemo so the cost is paid once per
  // input change, not every render.
  const filtered = useMemo(() => {
    let list = channels;
    if (category !== "all" && category !== "no-signal") {
      list = list.filter((c) => c.category === category);
    }
    if (debouncedSearch.trim()) {
      const q = debouncedSearch.trim().toLowerCase();
      list = list.filter((c) => {
        const np = getNowPlaying(scheduleByChannel[c.id]);
        return (
          c.name.toLowerCase().includes(q) ||
          (np?.title ?? "").toLowerCase().includes(q) ||
          (c.group_name ?? "").toLowerCase().includes(q)
        );
      });
    }

    // `slice()` so we never mutate the parent's array reference — the
    // memo upstream caches it and a sort-in-place would taint the
    // cached value.
    const sorted = list.slice();
    switch (sort) {
      case "ending": {
        // What's wrapping up first comes first — the user either jumps
        // on the tail or skips it for something newer.
        const endTime = (c: Channel) => {
          const np = getNowPlaying(scheduleByChannel[c.id]);
          return np ? new Date(np.end_time).getTime() : Number.POSITIVE_INFINITY;
        };
        sorted.sort((a, b) => endTime(a) - endTime(b));
        break;
      }
      case "starting": {
        // Channels whose next programme starts soonest. Useful at
        // 21:55 on the dot — surfaces "this starts in 5 minutes" picks.
        const nextStart = (c: Channel) => {
          const next = getUpNext(scheduleByChannel[c.id]);
          return next
            ? new Date(next.start_time).getTime()
            : Number.POSITIVE_INFINITY;
        };
        sorted.sort((a, b) => nextStart(a) - nextStart(b));
        break;
      }
      case "name": {
        sorted.sort((a, b) =>
          a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
        );
        break;
      }
      case "favorites":
      default: {
        sorted.sort((a, b) => {
          const aFav = favoriteSet.has(a.id) ? 0 : 1;
          const bFav = favoriteSet.has(b.id) ? 0 : 1;
          return aFav - bFav || a.number - b.number;
        });
      }
    }
    return sorted;
  }, [
    channels,
    category,
    debouncedSearch,
    sort,
    scheduleByChannel,
    favoriteSet,
  ]);

  // Pagination — render at most 60 cards on first paint, grow on scroll
  // via IntersectionObserver. With 22 k+ channels in some libraries,
  // mounting them all up-front locks the main thread for seconds; this
  // keeps the DOM bounded.
  const { visible, hasMore, sentinelRef, total } = usePagedItems(filtered, 60);

  return (
    <div className="flex flex-col gap-6">
      {/* Sticky bar — chips on the left, sort selector on the right.
          Anchored below the global TopBar so the user can switch
          category or re-rank without scrolling back up. */}
      <div
        className="sticky z-20 -mx-4 px-4 md:-mx-6 md:px-6"
        style={{ top: "var(--topbar-height)" }}
      >
        <div className="flex items-center gap-3 border-b border-tv-line/60 bg-tv-bg-0/85 py-2 backdrop-blur-xl">
          <div className="min-w-0 flex-1">
            <CategoryChips
              counts={counts}
              active={category}
              onChange={onCategoryChange}
            />
          </div>
          <label className="flex shrink-0 items-center gap-2 text-xs text-tv-fg-2">
            <span className="hidden md:inline">
              {t("liveTV.sortLabel", { defaultValue: "Ordenar" })}
            </span>
            <select
              value={sort}
              onChange={(e) => onSortChange(e.target.value as LiveNowSort)}
              className="rounded-full border border-tv-line bg-tv-bg-1 px-3 py-1 text-xs font-medium text-tv-fg-1 hover:border-tv-line-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/40"
              aria-label={t("liveTV.sortAria", {
                defaultValue: "Ordenar canales",
              })}
            >
              <option value="favorites">
                {t("liveTV.sort.favorites", { defaultValue: "Favoritos" })}
              </option>
              <option value="ending">
                {t("liveTV.sort.ending", { defaultValue: "Termina pronto" })}
              </option>
              <option value="starting">
                {t("liveTV.sort.starting", { defaultValue: "Empieza pronto" })}
              </option>
              <option value="name">
                {t("liveTV.sort.name", { defaultValue: "Nombre A-Z" })}
              </option>
            </select>
          </label>
        </div>
      </div>

      {filtered.length > 0 ? (
        <p className="text-xs text-tv-fg-3">
          {t("liveTV.showingCount", {
            defaultValue: "Mostrando {{visible}} de {{total}}",
            visible: visible.length,
            total,
          })}
        </p>
      ) : null}

      {filtered.length === 0 ? (
        // Empty-state copy reflects WHY nothing is on screen:
        //  1. There's a search query → "no match for X"
        //  2. There's a category filter → "no live channels in this cat"
        //  3. The pool is empty entirely → most likely no EPG loaded
        //     (point at the admin EPG sources panel) or genuinely a
        //     dead window (3 AM with everyone off-air).
        <div className="rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-10 text-center text-sm text-tv-fg-2">
          {search.trim() ? (
            t("liveTV.noLiveNowMatch", {
              defaultValue:
                "Ningún canal en directo coincide con la búsqueda.",
            })
          ) : category !== "all" && category !== "no-signal" ? (
            t("liveTV.noLiveNowInCategory", {
              defaultValue:
                "No hay canales en directo en esta categoría ahora mismo.",
            })
          ) : channels.length === 0 ? (
            <>
              <p className="font-medium text-tv-fg-1">
                {t("liveTV.noLiveNowAtAll", {
                  defaultValue: "No hay canales emitiendo ahora mismo.",
                })}
              </p>
              <p className="mt-2 text-xs text-tv-fg-3">
                {t("liveTV.noLiveNowHint", {
                  defaultValue:
                    "Si acabas de añadir canales, su guía EPG aún se está descargando. Puedes revisar las fuentes EPG en Bibliotecas.",
                })}
              </p>
            </>
          ) : (
            t("liveTV.noLiveNow", {
              defaultValue: "No hay canales emitiendo en este momento.",
            })
          )}
        </div>
      ) : (
        <div
          className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6 motion-safe:animate-fade-in"
          // Re-key on category/search/sort so the fade replays when the
          // user changes any of them — communicates "this is a new
          // set" without forcing the whole layout to re-mount.
          key={`${category}-${debouncedSearch}-${sort}`}
        >
          {visible.map((ch) => (
            <ChannelCard
              key={ch.id}
              channel={ch}
              nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
              upNext={getUpNext(scheduleByChannel[ch.id])}
              isFavorite={favoriteSet.has(ch.id)}
              onClick={() => onOpen(ch)}
              onToggleFavorite={() => onToggleFavorite(ch.id)}
            />
          ))}
        </div>
      )}

      {/* Sentinel — placed AFTER the grid (sibling, not child) so the
          IntersectionObserver fires regardless of grid layout. The
          400 px rootMargin in usePagedItems means we begin loading the
          next batch a screen-and-a-bit before the user actually
          reaches the bottom. */}
      {hasMore ? (
        <div ref={sentinelRef} aria-hidden="true" className="h-px w-full" />
      ) : null}
    </div>
  );
}
