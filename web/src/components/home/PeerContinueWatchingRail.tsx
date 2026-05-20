// PeerContinueWatchingRail -- cross-peer Continue Watching home rail.
//
// Backend: /api/v1/me/peers/continue-watching reads federation_progress
// JOIN federation_item_cache locally (no peer fan-out -- the state is
// ours; the metadata for the join was hydrated when the user browsed
// the catalog). The rail self-hides when nothing's in progress so a
// solo deployment renders home identically to pre-federation.

import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { federationItemToMediaItem } from "@/api/federationAdapter";
import type { MediaItem, PeerContinueWatchingItem } from "@/api/types";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

function usePeerContinueWatching() {
  return useQuery<PeerContinueWatchingItem[]>({
    queryKey: queryKeys.myPeersContinueWatching,
    queryFn: () => api.getPeerContinueWatching(),
    // Same cadence as the local Continue Watching rail -- a stale
    // window of a few seconds is fine, the source of truth lives
    // server-side and is upserted every 10 s by the player.
    staleTime: 30 * 1000,
  });
}

export function PeerContinueWatchingRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = usePeerContinueWatching();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail
        title={t("home.peerContinueWatching", {
          defaultValue: "Continue watching on peers",
        })}
      >
        {Array.from({ length: 5 }, (_, i) => (
          <div key={`peer-continue-skeleton-${i}`} className="w-[180px] md:w-[200px] lg:w-[220px] xl:w-[240px] shrink-0">
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-lg"
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
    <HomeRail
      title={t("home.peerContinueWatching", {
        defaultValue: "Continue watching on peers",
      })}
    >
      {items.map((it) => (
        <div
          key={`${it.peer_id}:${it.id}`}
          className="w-[180px] md:w-[200px] lg:w-[220px] xl:w-[240px] shrink-0"
        >
          <PosterCard
            item={rowToMediaItem(it)}
            href={hrefFor(it)}
            cornerBadge={
              <span
                className="inline-flex items-center gap-1 rounded-full bg-black/65 px-2 py-0.5 text-[10px] font-medium text-white shadow-sm backdrop-blur-sm"
                title={t("peers.sharedBy", { name: it.peer_name })}
              >
                <span className="size-1.5 rounded-full bg-emerald-400" aria-hidden />
                <span className="max-w-[100px] truncate">{it.peer_name}</span>
              </span>
            }
          />
        </div>
      ))}
    </HomeRail>
  );
}

// rowToMediaItem fills the MediaItem with the bits the PosterCard
// needs (poster, title, type) plus a user_data.progress envelope so
// the existing progress bar overlay shows up exactly like local rows.
function rowToMediaItem(row: PeerContinueWatchingItem): MediaItem {
  const base = federationItemToMediaItem({
    id: row.id,
    type: row.type,
    title: row.title,
    year: row.year,
    overview: row.overview,
    poster_url: row.poster_url,
  });
  return {
    ...base,
    duration_ticks: row.duration_ticks || base.duration_ticks,
    user_data: {
      progress: {
        position_ticks: row.position_ticks,
        percentage: row.percentage,
        audio_stream_index: null,
        subtitle_stream_index: null,
      },
      is_favorite: false,
      played: false,
      play_count: 0,
      last_played_at: row.last_played_at || null,
    },
  };
}

function hrefFor(row: PeerContinueWatchingItem): string {
  if (!row.library_id) return `/peers/${row.peer_id}`;
  return `/peers/${row.peer_id}/libraries/${row.library_id}/items/${row.id}`;
}
