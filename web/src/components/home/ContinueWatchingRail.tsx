// ContinueWatchingRail — landscape rail powered by the existing
// /me/continue-watching endpoint. Hides itself when empty, renders
// skeletons while loading, and uses the smart LandscapeCard so
// episodes show "S0XE0Y · Title" with the series name as the lead.

import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import {
  useContinueWatching,
  useMarkPlayed,
  useRemoveFromContinueWatching,
} from "@/api/hooks";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";
import { LandscapeCard } from "./LandscapeCard";

export function ContinueWatchingRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useContinueWatching();
  const markPlayed = useMarkPlayed();
  const remove = useRemoveFromContinueWatching();

  const handleMarkWatched = (item: MediaItem) => {
    markPlayed.mutate(item.id);
  };
  const handleRemove = (item: MediaItem) => {
    remove.mutate(item.id);
  };

  // Hide the rail entirely on error — the home page already shows
  // a generic error toast / refetch button if every query failed.
  // A single rail's failure shouldn't blank the whole shell.
  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail title={t("home.continueWatching")}>
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="w-[300px] md:w-[340px] lg:w-[380px] xl:w-[420px] shrink-0">
            <Skeleton
              variant="rectangular"
              className="aspect-[16/9] w-full rounded-md"
            />
            <Skeleton variant="text" width="80%" className="mt-2" />
          </div>
        ))}
      </HomeRail>
    );
  }

  const items = data ?? [];
  if (items.length === 0) return null;

  return (
    <HomeRail title={t("home.continueWatching")}>
      {items.map((item) => (
        // One uniform 16:9 shape for the whole rail — episodes use
        // their per-episode screencap, movies use their thumb_url
        // ("miniatura"), the landscape still TMDb / Fanart ship
        // alongside the cartel for exactly this kind of listing.
        // LandscapeCard handles the per-type image selection
        // internally so the rail itself stays trivial. Auto-play
        // on click because every card here is mid-watch by
        // definition — dropping the user on the detail page first
        // is friction nobody wants on a "resume" surface.
        <LandscapeCard
          key={item.id}
          item={item}
          autoPlay
          onMarkWatched={handleMarkWatched}
          onRemove={handleRemove}
        />
      ))}
    </HomeRail>
  );
}
