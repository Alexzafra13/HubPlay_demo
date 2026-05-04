import { useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import {
  useMyPeers,
  usePeerItems,
  usePeerLibraries,
  useRefreshPeerLibrary,
} from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { MediaGrid } from "@/components/media/MediaGrid";
import { federationItemToMediaItem } from "@/api/federationAdapter";
import type { FederationRemoteItem, MediaItem } from "@/api/types";

const PAGE_SIZE = 50;

// PeerLibraryItemsPage — paginated catalog of items in a peer's
// shared library, rendered with the SAME PosterCard + MediaGrid the
// local Movies / Series pages use. Plex-style mixed surfacing: the
// sidebar surfaces the peer's library as a top-level entry, this
// page renders it identically to a local library, only with proxied
// posters + a small "shared by Pedro" badge per card.
export default function PeerLibraryItemsPage() {
  const { t } = useTranslation();
  const { peerId = "", libraryId = "" } = useParams();
  const [page, setPage] = useState(0);

  const peers = useMyPeers();
  const libraries = usePeerLibraries(peerId);
  const items = usePeerItems(peerId, libraryId, page * PAGE_SIZE, PAGE_SIZE);
  const refresh = useRefreshPeerLibrary(peerId, libraryId);

  const peer = peers.data?.find((p) => p.id === peerId);
  const library = libraries.data?.find((l) => l.id === libraryId);

  // Adapt the peer's item rows to the canonical MediaItem shape so
  // PosterCard renders them like any local title. Memoised on the
  // raw items pointer + peerId so paging doesn't recompute O(n) per
  // render.
  const remoteItems: FederationRemoteItem[] = items.data?.items ?? [];
  const total = items.data?.total ?? 0;
  const fromCache = items.data?.from_cache ?? false;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const mediaItems = useMemo<MediaItem[]>(
    () => remoteItems.map(federationItemToMediaItem),
    [remoteItems],
  );

  const hrefFor = (item: MediaItem) =>
    `/peers/${peerId}/libraries/${libraryId}/items/${item.id}`;

  const cornerBadgeFor = peer
    ? () => (
        <span
          className="inline-flex items-center gap-1 rounded-full bg-black/65 px-2 py-0.5 text-[10px] font-medium text-white shadow-sm backdrop-blur-sm"
          title={t("peers.sharedBy", { name: peer.name })}
        >
          <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" aria-hidden />
          <span className="max-w-[120px] truncate">{peer.name}</span>
        </span>
      )
    : undefined;

  if (items.error) {
    return (
      <div className="p-6 sm:p-10">
        <Link to="/peers" className="text-sm text-accent hover:underline">
          ← {t("peers.backToList")}
        </Link>
        <p className="mt-4 rounded border border-danger/40 bg-danger/5 p-3 text-sm text-danger">
          {t("peers.unreachable", { error: String(items.error) })}
        </p>
      </div>
    );
  }

  return (
    <div className="p-6 sm:p-10">
      <Link to="/peers" className="text-sm text-accent hover:underline">
        ← {t("peers.backToList")}
      </Link>

      <header className="mt-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
              {library?.name ?? t("peers.unknownLibrary")}
            </h1>
            {library?.content_type && (
              <span className="rounded bg-bg-base px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-text-muted">
                {library.content_type}
              </span>
            )}
          </div>
          {peer && (
            <p className="mt-1 text-sm text-text-muted">
              <span className="inline-flex items-center gap-1.5">
                <span
                  className="h-1.5 w-1.5 rounded-full bg-emerald-500"
                  aria-hidden
                />
                {t("peers.sharedBy", { name: peer.name })}
              </span>
              <span className="mx-2 text-text-muted/50">·</span>
              <span>{t("peers.itemCount", { count: total })}</span>
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          {fromCache && (
            <span
              className="rounded bg-yellow-500/10 px-2 py-1 text-xs text-yellow-500"
              title={t("peers.cacheHint")}
            >
              {t("peers.cached")}
            </span>
          )}
          <Button
            variant="secondary"
            size="sm"
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending}
          >
            {refresh.isPending ? t("peers.refreshing") : t("peers.refresh")}
          </Button>
        </div>
      </header>

      <div className="mt-8">
        <MediaGrid
          items={mediaItems}
          loading={items.isLoading}
          emptyMessage={t("peers.emptyLibrary")}
          hrefFor={hrefFor}
          cornerBadgeFor={cornerBadgeFor}
        />
      </div>

      {totalPages > 1 && (
        <div className="mt-8 flex items-center justify-between gap-3">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setPage((p) => Math.max(0, p - 1))}
            disabled={page === 0}
          >
            ← {t("peers.prev")}
          </Button>
          <span className="text-xs text-text-muted">
            {t("peers.pageOf", { current: page + 1, total: totalPages })}
          </span>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
            disabled={page >= totalPages - 1}
          >
            {t("peers.next")} →
          </Button>
        </div>
      )}
    </div>
  );
}
