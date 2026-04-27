import { useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { usePagedItems } from "@/hooks/usePagedItems";
import type {
  Channel,
  ChannelCategory,
  ContinueWatchingChannel,
  EPGProgram,
  UnhealthyChannel,
} from "@/api/types";
import { CategoryChips, type CategoryFilter } from "./CategoryChips";
import { ChannelCard } from "./ChannelCard";
import { ChannelRail } from "./ChannelRail";
import { HeroSpotlight, type HeroSpotlightItem } from "./HeroSpotlight";
import type { HeroMode } from "./HeroSettings";
import { CHANNEL_CATEGORY_ORDER } from "./categoryOrder";
import { capitalize, getNowPlaying, getUpNext } from "./epgHelpers";

interface DiscoverViewProps {
  heroItems: HeroSpotlightItem[];
  heroLabel: string;
  counts: Record<CategoryFilter, number>;
  category: CategoryFilter;
  onCategoryChange: (c: CategoryFilter) => void;
  channelsByCategory: Map<ChannelCategory, Channel[]>;
  scheduleByChannel: Record<string, EPGProgram[]>;
  onOpen: (ch: Channel) => void;
  favoriteSet: Set<string>;
  onToggleFavorite: (channelId: string) => void;
  unhealthyChannels: UnhealthyChannel[];
  continueWatching: ContinueWatchingChannel[];
  /** Overlay rendered at the top-left of the hero — typically the page
   * title + counts. Lifted in here so the hero hugs the TopBar. */
  heroHeaderOverlay?: ReactNode;
  /** Current hero preference. When set to "off", the hero is hidden by
   * user choice — we render a discrete restore hint where the hero
   * would have been so the user is never confused about its absence. */
  heroMode?: HeroMode;
  onHeroModeChange?: (mode: HeroMode) => void;
}

/**
 * DiscoverView — the "Descubrir" tab body.
 *
 * Layout:
 *   1. HeroSpotlight at the top (driven by useHeroSpotlight in the parent).
 *   2. Category chips bar.
 *   3. Category rails — one per category with channels present. The order
 *      mirrors CHANNEL_CATEGORY_ORDER so chips and rails agree.
 *   4. "Apagados" rail (only when the health probe has flagged failing
 *      channels AND the user is on the "all" category). Dimmed cards,
 *      preview disabled, click still works in case the probe is stale.
 */
export function DiscoverView({
  heroItems,
  heroLabel,
  counts,
  category,
  onCategoryChange,
  channelsByCategory,
  scheduleByChannel,
  onOpen,
  favoriteSet,
  onToggleFavorite,
  unhealthyChannels,
  continueWatching,
  heroHeaderOverlay,
  heroMode,
  onHeroModeChange,
}: DiscoverViewProps) {
  const { t } = useTranslation();

  // "all" and "no-signal" are virtual filters spanning every category;
  // they render as a stack of category rails. A concrete category
  // switches to a vertical grid so the user can scan top-to-bottom
  // through every channel in that category without horizontal-scroll
  // fatigue.
  const isAggregate = category === "all" || category === "no-signal";
  // Cap each rail at 30 channels — beyond that horizontal scrolling
  // becomes a hostile interaction (you'd be clicking the chevron 30
  // times) AND the DOM cost of mounting 5000 cards per rail kills
  // navigation on big libraries. The clickable rail header doubles as
  // "see all 5000 in a vertical grid", which is the right surface for
  // exhaustive browsing.
  const RAIL_PREVIEW_SIZE = 30;
  const visibleRails = isAggregate
    ? CHANNEL_CATEGORY_ORDER.map(
        (c) =>
          [c, channelsByCategory.get(c) ?? ([] as Channel[])] as const,
      ).filter(([, list]) => list.length > 0)
    : [];
  // useMemo so the array reference stays stable across renders when
  // the inputs haven't changed — `usePagedItems` keys its
  // reset-on-input logic on identity, and a fresh `[]` literal each
  // render would loop it forever.
  const concreteList = useMemo<Channel[]>(() => {
    if (isAggregate) return [];
    return channelsByCategory.get(category as ChannelCategory) ?? [];
  }, [isAggregate, category, channelsByCategory]);

  // Vertical grid pagination — same model as LiveNowView. Caps DOM at
  // ~60 cards on first paint, grows on scroll.
  const {
    visible: concreteVisible,
    hasMore: concreteHasMore,
    sentinelRef: concreteSentinelRef,
  } = usePagedItems(concreteList, 60);

  // Continue-watching only renders on the "all" tab. Scoping it to a
  // single category (e.g. "sports") would surface channels outside
  // that filter, which defeats the chip selection.
  const showContinueWatching =
    category === "all" && continueWatching.length > 0;

  // Visible "hero is hidden" hint. The HeroSettings dropdown lets the
  // user pick "Ocultar destacado", which sets `heroMode` to "off" and
  // makes <HeroSpotlight> render null. Without this hint the hero just
  // disappears and the user has to remember the gear menu to bring it
  // back — which they don't. We only show the hint on Discover (this
  // component) because that's the surface the hero belongs to.
  const heroHidden = heroMode === "off";

  return (
    <div className="flex flex-col gap-8">
      {heroHidden ? (
        <div className="flex items-center justify-between gap-4 rounded-tv-md border border-dashed border-tv-line bg-tv-bg-1 px-4 py-3 text-xs text-tv-fg-2">
          <span>
            {t("liveTV.heroHidden", {
              defaultValue: "El destacado está oculto.",
            })}
          </span>
          {onHeroModeChange ? (
            <button
              type="button"
              onClick={() => onHeroModeChange("favorites")}
              className="rounded-full border border-accent/50 bg-accent-soft px-3 py-1 text-xs font-medium text-accent-light transition-colors hover:bg-accent/20"
            >
              {t("liveTV.heroRestore", { defaultValue: "Mostrar" })}
            </button>
          ) : null}
        </div>
      ) : (
        <HeroSpotlight
          items={heroItems}
          label={heroLabel}
          onOpen={onOpen}
          headerOverlay={heroHeaderOverlay}
        />
      )}

      {/* The "Ahora en directo" rail used to live here. We promoted
          inmediacy to its own first-class tab ("Ahora") so this
          surface goes back to its editorial job — hero + categories +
          rails — and doesn't duplicate the same data twice. */}

      {/* Sticky chip bar — pinned below the global TopBar so the
          category selector stays in view while the channel grid scrolls
          under it. Negative margins bleed to the page section's edges
          so the sticky band reaches both sides of the viewport. */}
      <div
        className="sticky z-20 -mx-4 px-4 md:-mx-6 md:px-6"
        style={{ top: "var(--topbar-height)" }}
      >
        <div className="border-b border-tv-line/60 bg-tv-bg-0/85 py-2 backdrop-blur-xl">
          <CategoryChips
            counts={counts}
            active={category}
            onChange={onCategoryChange}
          />
        </div>
      </div>

      {showContinueWatching ? (
        <ChannelRail
          title={t("liveTV.continueWatching", {
            defaultValue: "Continuar viendo",
          })}
          count={continueWatching.length}
        >
          {continueWatching.map((ch) => (
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
        </ChannelRail>
      ) : null}

      {/* Empty state — fires when the active filter has no channels.
          For aggregate filters (all/no-signal) that means no rails to
          render; for concrete categories it means the grid would be
          empty. Both share the same dashed-border treatment. */}
      {isAggregate && visibleRails.length === 0 && (
        <div className="rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-10 text-center text-sm text-tv-fg-2">
          {t("liveTV.noChannelsInCategory", {
            defaultValue: "No hay canales en esta categoría.",
          })}
        </div>
      )}
      {!isAggregate && concreteList.length === 0 && (
        <div className="rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-10 text-center text-sm text-tv-fg-2">
          {t("liveTV.noChannelsInCategory", {
            defaultValue: "No hay canales en esta categoría.",
          })}
        </div>
      )}

      {/* Aggregate filters keep the editorial "rails by category"
          layout — each row scrolls horizontally so the page surfaces a
          mix without dominating the fold. The keyed wrapper + the
          fade-in utility make the rails↔grid switch feel intentional
          (a fade) instead of a hard cut, but reduce-motion users skip
          the animation. */}
      <div
        key={isAggregate ? "rails" : `grid-${category}`}
        className="flex flex-col gap-8 motion-safe:animate-fade-in"
      >
      {isAggregate &&
        visibleRails.map(([cat, list]) => (
          <ChannelRail
            key={cat}
            title={t(`liveTV.category.${cat}`, {
              defaultValue: capitalize(cat),
            })}
            count={list.length}
            onSeeAll={
              category === "all" ? () => onCategoryChange(cat) : undefined
            }
          >
            {list.slice(0, RAIL_PREVIEW_SIZE).map((ch) => (
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
          </ChannelRail>
        ))}

      {/* Concrete category — flat vertical grid. Top-to-bottom flow so
          the user can sweep the whole category without left-arrow
          fatigue. Card width tracks the rail's 260 px so the visual
          rhythm survives switching between modes. */}
      {!isAggregate && concreteList.length > 0 && (
        <section className="flex flex-col gap-3">
          <header className="flex items-baseline justify-between gap-3">
            <h2 className="flex items-center gap-2 text-base font-semibold text-tv-fg-0">
              {t(`liveTV.category.${category}`, {
                defaultValue: capitalize(category),
              })}
              <span className="rounded-full bg-tv-bg-2 px-2 py-0.5 font-mono text-[10px] font-medium tabular-nums text-tv-fg-2">
                {concreteList.length}
              </span>
            </h2>
            <p className="text-xs text-tv-fg-3">
              {t("liveTV.showingCount", {
                defaultValue: "Mostrando {{visible}} de {{total}}",
                visible: concreteVisible.length,
                total: concreteList.length,
              })}
            </p>
          </header>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
            {concreteVisible.map((ch) => (
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
          {concreteHasMore ? (
            <div
              ref={concreteSentinelRef}
              aria-hidden="true"
              className="h-px w-full"
            />
          ) : null}
        </section>
      )}
      </div>

      {/* "Apagados" — channels the health probe has flagged as failing.
          The backend filters them out of the main channel list so
          Discover stays clean, but we surface them here, dimmed, at
          the bottom of the page. A click still tries to play (the
          probe might be stale); the rail fades to near-nothing when
          there's nothing to show, no hard empty state. */}
      {unhealthyChannels.length > 0 && category === "all" ? (
        <ChannelRail
          title={t("liveTV.category.apagados", { defaultValue: "Apagados" })}
          count={unhealthyChannels.length}
          subtitle={t("liveTV.apagadosSubtitle", {
            defaultValue:
              "Canales con fallos recientes; reintenta, quizá hayan vuelto.",
          })}
        >
          {unhealthyChannels.map((ch) => (
            <ChannelCard
              key={ch.id}
              channel={ch}
              isFavorite={favoriteSet.has(ch.id)}
              onClick={() => onOpen(ch)}
              onToggleFavorite={() => onToggleFavorite(ch.id)}
              previewOnHover={false}
              dimmed
            />
          ))}
        </ChannelRail>
      ) : null}
    </div>
  );
}
