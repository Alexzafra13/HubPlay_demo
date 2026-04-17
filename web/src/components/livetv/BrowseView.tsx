import type { ReactNode } from "react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { CategoryChip } from "./CategoryChip";
import { ChannelCard } from "./ChannelCard";
import type { FeaturedSlide } from "./FeaturedHero";
import { FeaturedHero } from "./FeaturedHero";
import { parseCategory } from "./categoryHelpers";
import { getNowPlaying, getUpNext } from "./epgHelpers";

interface BrowseViewProps {
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  channelsByCategory: Map<string, Channel[]>;
  categoryNames: string[];
  /** Search state — when active, the rest of the landing collapses down to
   *  just the result grid. */
  search: string;
  onSearchChange: (value: string) => void;
  activeCategory: string | null;
  onCategoryChange: (name: string | null) => void;
  activeChannelId: string | undefined;
  onSelectChannel: (channel: Channel) => void;
  lastChannelId: string | null;
  favorites: Set<string>;
  onToggleFavorite: (channelId: string) => void;
  onOpenGuide: () => void;
}

/**
 * The landing (non-watching) view: featured hero, "Continue watching",
 * favourites, "Airing now", then per-category shelves. Inspired by the
 * channel-browsing screens in Movistar+ and TDT Channels.
 */
export function BrowseView({
  channels,
  scheduleByChannel,
  channelsByCategory,
  categoryNames,
  search,
  onSearchChange,
  activeCategory,
  onCategoryChange,
  activeChannelId,
  onSelectChannel,
  lastChannelId,
  favorites,
  onToggleFavorite,
  onOpenGuide,
}: BrowseViewProps) {
  const { t } = useTranslation();

  // ── Derived sets ──────────────────────────────────────────────
  const liveNowChannels = useMemo(
    () =>
      channels.filter((c) => getNowPlaying(scheduleByChannel[c.id]) !== null),
    [channels, scheduleByChannel],
  );

  const featuredSlides = useMemo<FeaturedSlide[]>(() => {
    // Prefer channels from varied categories so the hero never loops over
    // a monoculture. If we have EPG data, use currently-airing programmes;
    // otherwise fall back to branded slides (logo + category) so the hero
    // still feels populated when the iptv-org feed ships no EPG URL.
    const byCategory = new Map<string, FeaturedSlide>();
    const pool = liveNowChannels.length > 0 ? liveNowChannels : channels;
    for (const c of pool) {
      const key = parseCategory(c.group).primary;
      if (byCategory.has(key)) continue;
      const program = getNowPlaying(scheduleByChannel[c.id]);
      byCategory.set(key, { channel: c, program });
      if (byCategory.size >= 6) break;
    }
    return Array.from(byCategory.values());
  }, [liveNowChannels, channels, scheduleByChannel]);

  const lastChannel = useMemo(
    () => channels.find((c) => c.id === lastChannelId) ?? null,
    [channels, lastChannelId],
  );

  const favoriteChannels = useMemo(() => {
    if (favorites.size === 0) return [];
    return channels.filter((c) => favorites.has(c.id));
  }, [channels, favorites]);

  const searchResults = useMemo(() => {
    if (!search) return [];
    const q = search.toLowerCase();
    return channels.filter(
      (ch) =>
        ch.name.toLowerCase().includes(q) ||
        (ch.group ?? "").toLowerCase().includes(q) ||
        parseCategory(ch.group).primary.toLowerCase().includes(q),
    );
  }, [channels, search]);

  const displayCategoryChannels = activeCategory
    ? (channelsByCategory.get(activeCategory) ?? [])
    : [];

  // ── Render helpers ────────────────────────────────────────────
  function renderChannelTile(ch: Channel) {
    return (
      <ChannelCard
        channel={ch}
        isActive={activeChannelId === ch.id}
        nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
        upNext={getUpNext(scheduleByChannel[ch.id])}
        onClick={() => onSelectChannel(ch)}
        isFavorite={favorites.has(ch.id)}
        onToggleFavorite={() => onToggleFavorite(ch.id)}
      />
    );
  }

  return (
    <div className="flex flex-col gap-8 px-4 pb-16 pt-4 md:px-6">
      {/* ── Search + guide toolbar ───────────────────────────── */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <div className="relative flex-1">
          <svg
            width="16"
            height="16"
            viewBox="0 0 20 20"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary"
            aria-hidden="true"
          >
            <circle cx="8.5" cy="8.5" r="5" />
            <path d="M12.5 12.5L17 17" />
          </svg>
          <label className="sr-only" htmlFor="channel-search">
            {t("liveTV.searchPlaceholder")}
          </label>
          <input
            id="channel-search"
            type="text"
            placeholder={t("liveTV.searchPlaceholder")}
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            className="w-full rounded-xl border border-white/10 bg-white/5 py-2.5 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted transition-all focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
          />
        </div>
        <button
          type="button"
          onClick={onOpenGuide}
          className="inline-flex shrink-0 items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/[0.03] px-4 py-2.5 text-sm font-semibold text-text-primary transition-colors hover:bg-white/[0.08]"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <rect x="3" y="4" width="18" height="16" rx="2" />
            <path d="M8 2v4M16 2v4M3 10h18M8 14h.01M12 14h.01M16 14h.01M8 18h.01M12 18h.01M16 18h.01" />
          </svg>
          {t("liveTV.openGuide")}
        </button>
      </div>

      {/* ── Search takeover ──────────────────────────────────── */}
      {search ? (
        searchResults.length === 0 ? (
          <div className="py-16 text-center text-text-muted">
            {t("liveTV.noChannelsMatch", { search })}
          </div>
        ) : (
          <section>
            <SectionHeader
              title={t("liveTV.searchResults")}
              count={searchResults.length}
            />
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:gap-5 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
              {searchResults.map((ch) => (
                <div key={ch.id}>{renderChannelTile(ch)}</div>
              ))}
            </div>
          </section>
        )
      ) : activeCategory ? (
        <section>
          <SectionHeader
            title={activeCategory}
            count={displayCategoryChannels.length}
            onSeeAll={() => onCategoryChange(null)}
            seeAllLabel={t("liveTV.backToAll")}
          />
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {displayCategoryChannels.map((ch) => (
              <div key={ch.id}>{renderChannelTile(ch)}</div>
            ))}
          </div>
        </section>
      ) : (
        <>
          {/* ── Featured hero ─────────────────────────────────── */}
          {featuredSlides.length > 0 && (
            <FeaturedHero slides={featuredSlides} onWatch={onSelectChannel} />
          )}

          {/* ── Category filter rail ─────────────────────────── */}
          <div className="scrollbar-hide -mx-4 flex gap-1.5 overflow-x-auto px-4 pb-1 md:-mx-6 md:px-6">
            <CategoryChip
              label={t("liveTV.all")}
              icon="✨"
              count={channels.length}
              active={activeCategory === null}
              onClick={() => onCategoryChange(null)}
            />
            {categoryNames.map((name) => (
              <CategoryChip
                key={name}
                label={name}
                count={channelsByCategory.get(name)?.length ?? 0}
                active={false}
                onClick={() => onCategoryChange(name)}
              />
            ))}
          </div>

          {/* ── Continue watching ──────────────────────────────
              When there's only one item (common case — most users rewatch
              the same channel), we render it as a wide banner card so the
              shelf doesn't look oddly empty. */}
          {lastChannel && (
            <section>
              <SectionHeader title={t("liveTV.continueWatching")} />
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <div className="sm:col-span-2 lg:col-span-1">
                  {renderChannelTile(lastChannel)}
                </div>
              </div>
            </section>
          )}

          {/* ── Favourites ───────────────────────────────────── */}
          {favoriteChannels.length > 0 && (
            <section>
              <SectionHeader
                title={t("liveTV.favorites")}
                count={favoriteChannels.length}
              />
              <Shelf>
                {favoriteChannels.map((ch) => (
                  <ShelfItem key={ch.id}>{renderChannelTile(ch)}</ShelfItem>
                ))}
              </Shelf>
            </section>
          )}

          {/* ── Airing now ───────────────────────────────────── */}
          {liveNowChannels.length > 0 && (
            <section>
              <SectionHeader
                title={t("liveTV.airingNow")}
                count={liveNowChannels.length}
                pulse
              />
              <Shelf>
                {liveNowChannels.slice(0, 20).map((ch) => (
                  <ShelfItem key={ch.id}>{renderChannelTile(ch)}</ShelfItem>
                ))}
              </Shelf>
            </section>
          )}

          {/* ── Per-category shelves ─────────────────────────── */}
          {categoryNames.map((name) => {
            const groupChannels = channelsByCategory.get(name) ?? [];
            return (
              <section key={name}>
                <SectionHeader
                  title={name}
                  count={groupChannels.length}
                  onSeeAll={() => onCategoryChange(name)}
                />
                <Shelf>
                  {groupChannels.map((ch) => (
                    <ShelfItem key={ch.id}>{renderChannelTile(ch)}</ShelfItem>
                  ))}
                </Shelf>
              </section>
            );
          })}
        </>
      )}
    </div>
  );
}

/**
 * Horizontal, swipe-friendly shelf. Snaps to each tile so mobile fling
 * gestures land cleanly and the last item never falls off at a fraction of
 * a card width. Desktop users can still drag-scroll with pointer events.
 */
function Shelf({ children }: { children: ReactNode }) {
  return (
    <div className="scrollbar-hide -mx-4 flex snap-x snap-mandatory gap-4 overflow-x-auto px-4 pb-2 md:-mx-6 md:gap-5 md:px-6">
      {children}
    </div>
  );
}

function ShelfItem({ children }: { children: ReactNode }) {
  return (
    <div className="w-48 shrink-0 snap-start sm:w-52 md:w-56 lg:w-60">
      {children}
    </div>
  );
}

function SectionHeader({
  title,
  count,
  onSeeAll,
  seeAllLabel,
  pulse = false,
}: {
  title: string;
  count?: number;
  onSeeAll?: () => void;
  seeAllLabel?: string;
  pulse?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="mb-3 flex items-center justify-between">
      <div className="flex items-baseline gap-2">
        <h2 className="text-base font-bold text-text-primary md:text-lg">
          {title}
        </h2>
        {typeof count === "number" && (
          <span className="flex items-center gap-1 rounded-full bg-white/5 px-2 py-0.5 text-[11px] font-semibold tabular-nums text-text-secondary">
            {pulse && (
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-live" />
            )}
            {count}
          </span>
        )}
      </div>
      {onSeeAll && (
        <button
          type="button"
          onClick={onSeeAll}
          className="text-xs font-medium text-accent-light transition-colors hover:text-accent"
        >
          {seeAllLabel ?? t("common.seeAll")} →
        </button>
      )}
    </div>
  );
}
