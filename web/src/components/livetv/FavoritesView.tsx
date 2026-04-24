import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelCard } from "./ChannelCard";
import { getNowPlaying, getUpNext } from "./epgHelpers";

interface FavoritesViewProps {
  channels: Channel[];
  favoriteSet: Set<string>;
  scheduleByChannel: Record<string, EPGProgram[]>;
  onOpen: (ch: Channel) => void;
  onToggleFavorite: (channelId: string) => void;
}

/**
 * FavoritesView — renders the user's favorite channels as a responsive
 * grid of ChannelCards. Derives the list from the current library's
 * channels filtered through `favoriteSet` so stale favorites (channels
 * removed by an M3U refresh) disappear automatically.
 *
 * We derive client-side rather than re-query the `/favorites/channels`
 * endpoint because it lets the grid share the same Channel objects already
 * loaded for Discover — keeping the cache tight and avoiding a second
 * fetch when both tabs are visited in one session.
 */
export function FavoritesView({
  channels,
  favoriteSet,
  scheduleByChannel,
  onOpen,
  onToggleFavorite,
}: FavoritesViewProps) {
  const { t } = useTranslation();
  const favorites = useMemo(
    () => channels.filter((c) => favoriteSet.has(c.id)),
    [channels, favoriteSet],
  );

  if (favorites.length === 0) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center gap-2 rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-8 text-center text-sm text-tv-fg-2">
        <div className="text-4xl" aria-hidden="true">
          ♡
        </div>
        <p>
          {t("liveTV.favoritesEmpty", {
            defaultValue:
              "Aún no tienes favoritos. Toca ♥ en cualquier canal para añadirlo.",
          })}
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
      {favorites.map((ch) => (
        <ChannelCard
          key={ch.id}
          channel={ch}
          nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
          upNext={getUpNext(scheduleByChannel[ch.id])}
          isFavorite
          onClick={() => onOpen(ch)}
          onToggleFavorite={() => onToggleFavorite(ch.id)}
        />
      ))}
    </div>
  );
}
