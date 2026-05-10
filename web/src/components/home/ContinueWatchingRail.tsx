// ContinueWatchingRail — landscape rail powered by the existing
// /me/continue-watching endpoint. Hides itself when empty, renders
// skeletons while loading, and uses the smart LandscapeCard so
// episodes show "S0XE0Y · Title" with the series name as the lead.

import { useTranslation } from "react-i18next";
import { useContinueWatching } from "@/api/hooks";
import { Skeleton } from "@/components/common";
import { PosterCard } from "@/components/media";
import { HomeRail } from "./HomeRail";
import { LandscapeCard } from "./LandscapeCard";

export function ContinueWatchingRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useContinueWatching();

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
      {items.map((item) =>
        // Movies use their poster (vertical 2:3) so the user
        // recognises the cartel they're used to — backdrops are
        // marketing-wide images that often share visual language
        // with other titles in the same franchise and are harder
        // to scan at a glance. Episodes stay on the landscape
        // card with their per-episode screencap (the still you'd
        // expect to see for a "what was this episode about" hint).
        // Both cards launch playback on click — the rail is
        // "resume" by definition; dropping the user on the detail
        // page first is friction nobody wants here.
        item.type === "movie" ? (
          <div
            key={item.id}
            className="w-[180px] md:w-[200px] lg:w-[220px] shrink-0"
          >
            <PosterCard item={item} href={`/movies/${item.id}?play=1`} />
          </div>
        ) : (
          <LandscapeCard key={item.id} item={item} autoPlay />
        ),
      )}
    </HomeRail>
  );
}
