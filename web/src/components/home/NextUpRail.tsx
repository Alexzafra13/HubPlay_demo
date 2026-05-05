// NextUpRail — landscape rail of the next episode for each series the
// user is mid-way through. Powered by the existing /me/next-up
// endpoint. Hides itself when empty.

import { useTranslation } from "react-i18next";
import { useNextUp } from "@/api/hooks";
import { EpisodeCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

export function NextUpRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useNextUp();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail title={t("home.nextUp")}>
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="w-[300px] md:w-[340px] lg:w-[380px] xl:w-[420px] shrink-0">
            <Skeleton
              variant="rectangular"
              className="aspect-video w-full rounded-md"
            />
            <Skeleton variant="text" width="70%" className="mt-2" />
            <Skeleton variant="text" width="40%" className="mt-1" />
          </div>
        ))}
      </HomeRail>
    );
  }

  const items = data ?? [];
  if (items.length === 0) return null;

  return (
    <HomeRail title={t("home.nextUp")} linkTo="/series">
      {items.map((item) => (
        <div key={item.id} className="w-[300px] md:w-[340px] lg:w-[380px] xl:w-[420px] shrink-0">
          <EpisodeCard item={item} />
        </div>
      ))}
    </HomeRail>
  );
}
