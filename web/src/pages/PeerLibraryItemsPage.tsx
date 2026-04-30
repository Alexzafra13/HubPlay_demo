import { lazy, Suspense, useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import {
  useMyPeers,
  usePeerItems,
  usePeerLibraries,
  useRefreshPeerLibrary,
} from "@/api/hooks/federation";
import { Spinner } from "@/components/common";
import { Button } from "@/components/common/Button";
import type { FederationRemoteItem } from "@/api/types";
import { usePeerPlayback } from "./peerPlayback";

// Lazy-load the player so the peer browse path doesn't pull hls.js + the
// player chunk on initial render. Only when the user actually clicks
// Play does the network fetch the player bundle.
const VideoPlayer = lazy(() =>
  import("@/components/player").then((m) => ({ default: m.VideoPlayer })),
);

const PAGE_SIZE = 50;

// PeerLibraryItemsPage — paginated catalog of items in a peer's
// shared library. Layout matches the local Movies/Series feel: poster-
// less grid for now (Phase 5+ will add poster proxying), but with
// proper card spacing, peer attribution badge, and clear navigation.
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

  // Phase 5 — viewer-side playback for peer items. The hook owns the
  // session lifecycle (POST start, DELETE on close); the page just
  // renders the overlay when showPlayer flips on.
  const playback = usePeerPlayback();

  if (items.isLoading) {
    return (
      <div className="p-6 sm:p-10">
        <Spinner />
      </div>
    );
  }
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

  const data = items.data;
  const itemList = data?.items ?? [];
  const total = data?.total ?? 0;
  const fromCache = data?.from_cache ?? false;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

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

      {itemList.length === 0 ? (
        <div className="mt-8 rounded-lg border border-dashed border-border bg-bg-elevated p-8 text-center">
          <p className="text-sm text-text-muted">{t("peers.emptyLibrary")}</p>
        </div>
      ) : (
        <ul className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {itemList.map((item) => (
            <ItemCard
              key={item.id}
              item={item}
              peerName={peer?.name ?? ""}
              onPlay={() => playback.play(peerId, item.id)}
              isStarting={playback.isLoading && playback.playingItemId === item.id}
            />
          ))}
        </ul>
      )}

      {playback.error && (
        <div
          role="alert"
          className="mt-4 rounded border border-red-500/40 bg-red-500/10 p-3 text-sm text-text-primary"
        >
          {playback.error}
        </div>
      )}

      {/* Inline player overlay. Mirrors the structure used on the
          local item-detail page: full-screen fixed div, escape via the
          player's own onClose. The Suspense fallback is a black void —
          loading the player chunk takes <100ms on a warm cache; a
          spinner here would be more flicker than help. */}
      {playback.showPlayer && playback.source && playback.playingItemId && (
        <div className="fixed inset-0 z-50 bg-black">
          <Suspense fallback={<div className="h-full w-full bg-black" />}>
            <VideoPlayer
              itemId={playback.playingItemId}
              sessionToken=""
              masterPlaylistUrl={playback.source.masterPlaylistUrl}
              directUrl={playback.source.directUrl}
              playbackMethod={playback.source.playbackMethod}
              title={
                itemList.find((i) => i.id === playback.playingItemId)?.title
              }
              onClose={() => void playback.close()}
            />
          </Suspense>
        </div>
      )}

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

// ItemCard — a single title in the grid. Mimics the local Movies
// page poster-card aesthetic without needing actual poster proxying
// (that's Phase 5+ when item-detail wire format ships). For now the
// title + year + type chip + truncated overview is enough signal.
function ItemCard({
  item,
  peerName,
  onPlay,
  isStarting,
}: {
  item: FederationRemoteItem;
  peerName: string;
  onPlay: () => void;
  isStarting: boolean;
}) {
  return (
    <li className="group flex flex-col gap-2 rounded-lg border border-border bg-bg-elevated p-4 transition-colors hover:border-accent">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-base font-semibold text-text-primary group-hover:text-accent">
            {item.title}
          </h3>
          {item.year ? (
            <p className="text-xs text-text-muted">{item.year}</p>
          ) : null}
        </div>
        <span className="shrink-0 rounded bg-bg-base px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-text-muted">
          {item.type}
        </span>
      </div>
      {item.overview && (
        <p className="line-clamp-3 text-xs text-text-muted">
          {item.overview}
        </p>
      )}
      <div className="mt-auto flex items-center justify-between pt-2">
        {peerName ? (
          <p className="text-[10px] text-text-muted/70">
            <span className="inline-flex items-center gap-1">
              <span
                className="h-1 w-1 rounded-full bg-emerald-500"
                aria-hidden
              />
              {peerName}
            </span>
          </p>
        ) : (
          <span />
        )}
        <Button size="sm" onClick={onPlay} disabled={isStarting}>
          {isStarting ? "…" : "▶"}
        </Button>
      </div>
    </li>
  );
}
