// ContinueWatchingRail — landscape rail powered by the existing
// /me/continue-watching endpoint. Hides itself when empty, renders
// skeletons while loading, and uses the smart LandscapeCard so
// episodes show "S0XE0Y · Title" with the series name as the lead.

import { useTranslation } from "react-i18next";
import { useContinueWatching } from "@/api/hooks";
import { Skeleton } from "@/components/common";
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
      {items.map((item) => (
        // Continue Watching cards always launch playback on click —
        // by definition the user is mid-watch, so dropping them on
        // the detail page first is one click of friction nobody
        // wants. The detail surface is one back-arrow away if they
        // really want metadata.
        <LandscapeCard key={item.id} item={item} autoPlay />
      ))}
    </HomeRail>
  );
}
