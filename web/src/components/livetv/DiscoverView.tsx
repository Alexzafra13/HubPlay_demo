import { useTranslation } from "react-i18next";
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
}: DiscoverViewProps) {
  const { t } = useTranslation();

  const visibleRails =
    category === "all"
      ? CHANNEL_CATEGORY_ORDER.map(
          (c) => [c, channelsByCategory.get(c) ?? []] as const,
        ).filter(([, list]) => list.length > 0)
      : [[category, channelsByCategory.get(category) ?? []] as const];

  // Continue-watching only renders on the "all" tab. Scoping it to a
  // single category (e.g. "sports") would surface channels outside
  // that filter, which defeats the chip selection.
  const showContinueWatching =
    category === "all" && continueWatching.length > 0;

  return (
    <div className="flex flex-col gap-8">
      <HeroSpotlight items={heroItems} label={heroLabel} onOpen={onOpen} />

      <CategoryChips
        counts={counts}
        active={category}
        onChange={onCategoryChange}
      />

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

      {visibleRails.length === 0 && (
        <div className="rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-10 text-center text-sm text-tv-fg-2">
          {t("liveTV.noChannelsInCategory", {
            defaultValue: "No hay canales en esta categoría.",
          })}
        </div>
      )}

      {visibleRails.map(([cat, list]) => (
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
          {list.map((ch) => (
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
