// LatestInLibraryRail — portrait-poster rail showing the most recently
// added items in one library. Reuses the existing
// /api/v1/items/latest?library_id=X endpoint via the
// `useLatestItems(libraryId)` hook — no new endpoint needed.
//
// The rail title is provided by the home layout (server-side resolved
// from libraries.name) so a library rename is reflected without a
// second round-trip.

import { useTranslation } from "react-i18next";
import { useLatestItems, useLibraries } from "@/api/hooks";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

interface LatestInLibraryRailProps {
  libraryId: string;
  libraryName: string;
}

export function LatestInLibraryRail({
  libraryId,
  libraryName,
}: LatestInLibraryRailProps) {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useLatestItems(libraryId);

  // Look up the library to derive the click target. Lazy: the
  // libraries list is already cached app-wide so this rides on the
  // existing fetch with no extra round-trip.
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryId);

  if (isError) return null;

  // Title pattern matches Jellyfin's "Latest in <Library>" so users
  // who migrate over recognise the section instantly.
  const title = t("home.latestIn", {
    library: libraryName,
    defaultValue: `Reciente en ${libraryName}`,
  });

  // The /items/latest endpoint returns every item type — including
  // seasons under a series. In a "Latest in series" rail seasons
  // duplicate their parent (you'd see Daredevil's poster as the
  // series and again as season 1). Keep only top-level catalog
  // items so each title appears once. Episodes get folded the same
  // way — they belong on the series detail page, not the home rail.
  const filtered = (data ?? []).filter(
    (i) => i.type === "movie" || i.type === "series",
  );

  // Click target for the rail title — landing page filtered by
  // content type. We don't deep-link the library yet (the catalog
  // pages don't accept a library param), so the click lands on the
  // overall Movies / Series listing which is the closest match.
  const linkTo =
    library?.content_type === "movies"
      ? "/movies"
      : library?.content_type === "shows"
        ? "/series"
        : undefined;

  if (isLoading) {
    return (
      <HomeRail title={title} linkTo={linkTo}>
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className="w-[150px] sm:w-[170px] shrink-0">
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-lg"
            />
            <Skeleton variant="text" width="80%" className="mt-2" />
            <Skeleton variant="text" width="50%" className="mt-1" />
          </div>
        ))}
      </HomeRail>
    );
  }

  if (filtered.length === 0) return null;

  return (
    <HomeRail title={title} linkTo={linkTo}>
      {filtered.map((item) => (
        <div key={item.id} className="w-[150px] sm:w-[170px] shrink-0">
          <PosterCard item={item} />
        </div>
      ))}
    </HomeRail>
  );
}
