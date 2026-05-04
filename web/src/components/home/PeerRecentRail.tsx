// PeerRecentRail — "Recently added on peers" home rail. Plex-style
// federation: items from every paired peer mix into the home page
// alongside local rails, with a small "Shared by Pedro" badge over
// each poster so the user always knows the origin.
//
// Backend: /api/v1/me/peers/recent fans out to every paired peer
// with a per-peer timeout; offline / errored peers are silently
// skipped. We reuse FederationSearchHit because the wire shape is
// identical (peer attribution + library_id + slim item).

import { useTranslation } from "react-i18next";
import { usePeerRecent } from "@/api/hooks/federation";
import { federationItemToMediaItem } from "@/api/federationAdapter";
import type { FederationSearchHit, MediaItem } from "@/api/types";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

export function PeerRecentRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = usePeerRecent();

  // Errors here are non-fatal: the rest of the home page still
  // renders. The fan-out itself never bubbles a 5xx unless the local
  // server's federation layer is broken — peer-side errors are
  // already absorbed into "0 hits".
  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail title={t("home.peerRecent", { defaultValue: "Recently added on peers" })}>
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className="w-[150px] sm:w-[170px] shrink-0">
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

  const hits = data?.hits ?? [];
  // Hide the rail entirely when no peer answered with anything.
  // Cold-start (no paired peers, all offline) renders the home page
  // exactly like pre-federation — no empty section, no spinner ghost.
  if (hits.length === 0) return null;

  return (
    <HomeRail title={t("home.peerRecent", { defaultValue: "Recently added on peers" })}>
      {hits.map((hit) => (
        <div
          key={`${hit.peer_id}:${hit.id}`}
          className="w-[150px] sm:w-[170px] shrink-0"
        >
          <PosterCard
            item={hitToMediaItem(hit)}
            href={hrefForHit(hit)}
            cornerBadge={
              <span
                className="inline-flex items-center gap-1 rounded-full bg-black/65 px-2 py-0.5 text-[10px] font-medium text-white shadow-sm backdrop-blur-sm"
                title={t("peers.sharedBy", { name: hit.peer_name })}
              >
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" aria-hidden />
                <span className="max-w-[100px] truncate">{hit.peer_name}</span>
              </span>
            }
          />
        </div>
      ))}
    </HomeRail>
  );
}

// hitToMediaItem reuses the federation adapter so we go through one
// canonical place that fills MediaItem defaults for federated items.
// The search-hit shape is wire-compatible with FederationRemoteItem
// for the fields the adapter reads (id, type, title, year, overview,
// poster_url) — the extras (peer_id, peer_name, library_id) are
// ignored by the adapter.
function hitToMediaItem(hit: FederationSearchHit): MediaItem {
  return federationItemToMediaItem({
    id: hit.id,
    type: hit.type,
    title: hit.title,
    year: hit.year,
    overview: hit.overview,
    poster_url: hit.poster_url,
  });
}

function hrefForHit(hit: FederationSearchHit): string {
  if (!hit.library_id) return `/peers/${hit.peer_id}`;
  return `/peers/${hit.peer_id}/libraries/${hit.library_id}/items/${hit.id}`;
}
