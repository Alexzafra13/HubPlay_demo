import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import {
  useMyPeers,
  usePeerItems,
  usePeerLibraries,
} from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { Spinner, EmptyState } from "@/components/common";
import { VideoPlayer } from "@/components/player";
import type {
  FederationRemoteItem,
  FederationRemoteItemsResponse,
  PeerItemProgress,
  PlaybackMethod,
} from "@/api/types";

const TICKS_PER_SECOND = 10_000_000;

// PeerItemDetail — Plex-style detail page for an item that lives on
// a federated peer. Compared to the local ItemDetail, the metadata
// surface is intentionally lean: the federation wire shape only
// carries title / year / type / overview / poster_url. Cast, ratings,
// chapters, episodes, and the per-user history all stay on the peer.
//
// We surface what we have (poster + title + overview) and a single
// canonical action: Play. When the user clicks it, we ask our origin
// to broker a stream session with the peer, get back a same-origin
// HLS master URL, and feed it into the same VideoPlayer the local
// playback path uses. The peer's hostname never reaches the user's
// browser -- all media flows through us.
export default function PeerItemDetail() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { peerId = "", libraryId = "", itemId = "" } = useParams();

  const peers = useMyPeers();
  const libraries = usePeerLibraries(peerId);
  // Pull the first page of items for this library; if the item the
  // user clicked is in a later page, we fall back to walking the
  // React Query cache (the previous page they came from will still
  // be hot). Accepting offset=0 here is the common case -- catalogs
  // typically fit a single page in the federation v1 size window.
  const items = usePeerItems(peerId, libraryId, 0, 50);

  const peer = peers.data?.find((p) => p.id === peerId);
  const library = libraries.data?.find((l) => l.id === libraryId);

  // findItem walks every cached items page for this (peer, library)
  // pair and returns the first match. The query key shape from
  // queryKeys.myPeerItems is ["me","peers",peerID,"libraries",libraryID,"items",offset]
  // -- we filter by prefix so any cached page contributes.
  const item = useMemo<FederationRemoteItem | undefined>(() => {
    const cached = queryClient.getQueriesData<FederationRemoteItemsResponse>({
      queryKey: ["me", "peers", peerId, "libraries", libraryId, "items"],
    });
    for (const [, value] of cached) {
      if (!value) continue;
      const found = value.items.find((it) => it.id === itemId);
      if (found) return found;
    }
    // Fallback: look at the page we just fetched in case the cache
    // walk above missed it (e.g. mounting directly via deep link).
    return items.data?.items.find((it) => it.id === itemId);
  }, [queryClient, peerId, libraryId, itemId, items.data]);

  // ─── Player overlay state ────────────────────────────────────────

  const [showPlayer, setShowPlayer] = useState(false);
  const [playerInfo, setPlayerInfo] = useState<{
    masterUrl: string;
    method: PlaybackMethod;
    startPosition: number;
  } | null>(null);
  const [playError, setPlayError] = useState<string | null>(null);

  // Resume support: fetch the user's stored position for this remote
  // item once on mount. Server returns the all-zero default when
  // nothing's been stored, so we don't have to special-case 404.
  const [progress, setProgress] = useState<PeerItemProgress | null>(null);
  useEffect(() => {
    if (!peerId || !itemId) return;
    let cancelled = false;
    api
      .getPeerItemProgress(peerId, itemId)
      .then((p) => {
        if (!cancelled) setProgress(p);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [peerId, itemId]);

  const resumeSeconds = useMemo(() => {
    if (!progress || progress.completed) return 0;
    if (progress.position_ticks <= 0) return 0;
    // Skip resume offers when the duration hasn't been recorded yet
    // (first save before the manifest landed) -- a near-zero
    // position_ticks alone is not enough to commit to "resume" UX.
    if (progress.duration_ticks > 0) {
      const pct = progress.position_ticks / progress.duration_ticks;
      if (pct >= 0.9) return 0;
    }
    return progress.position_ticks / TICKS_PER_SECOND;
  }, [progress]);

  const startPlayback = useCallback(
    async (startSeconds: number) => {
      if (!peerId || !itemId) return;
      setPlayError(null);
      try {
        const resp = await api.startPeerStreamSession(peerId, itemId);
        // resp.master_playlist_url is already same-origin (the server
        // synthesized it that way). VideoPlayer takes the URL verbatim
        // and lets useHls / hls.js resolve relative variant URLs
        // against it -- exactly the same as the local stream flow.
        const method: PlaybackMethod =
          resp.strategy === "direct_play"
            ? "direct_play"
            : resp.strategy === "direct_stream"
            ? "direct_stream"
            : "transcode";
        setPlayerInfo({
          masterUrl: resp.master_playlist_url,
          method,
          startPosition: startSeconds,
        });
        setShowPlayer(true);
      } catch (err) {
        setPlayError(t("peers.playFailed", { error: String(err) }));
      }
    },
    [peerId, itemId, t],
  );

  const handlePlay = useCallback(() => startPlayback(0), [startPlayback]);
  const handleResume = useCallback(
    () => startPlayback(resumeSeconds),
    [startPlayback, resumeSeconds],
  );

  const handleClosePlayer = useCallback(() => {
    setShowPlayer(false);
    setPlayerInfo(null);
    // Refresh progress so a follow-up Resume reflects what was just
    // saved on close. Fire-and-forget; the keepalive write from
    // useProgressReporter has already raced ahead of this.
    if (peerId && itemId) {
      api.getPeerItemProgress(peerId, itemId).then(setProgress).catch(() => {});
    }
  }, [peerId, itemId]);

  // ─── Render ──────────────────────────────────────────────────────

  if (items.isLoading && !item) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!item) {
    return (
      <div className="p-6 sm:p-10">
        <Link
          to={`/peers/${peerId}/libraries/${libraryId}`}
          className="text-sm text-accent hover:underline"
        >
          ← {t("peers.backToLibrary")}
        </Link>
        <div className="mt-6">
          <EmptyState
            title={t("peers.itemNotFoundTitle")}
            description={t("peers.itemNotFoundDescription")}
          />
        </div>
      </div>
    );
  }

  const backLink = `/peers/${peerId}/libraries/${libraryId}`;

  return (
    <div className="p-6 sm:p-10">
      <Link to={backLink} className="text-sm text-accent hover:underline">
        ← {library?.name ?? t("peers.backToLibrary")}
      </Link>

      <div className="mt-6 grid gap-8 md:grid-cols-[260px_1fr] lg:grid-cols-[300px_1fr]">
        {/* Poster column. Aspect-ratio reserved up-front so the
            layout doesn't reflow when the image decodes. */}
        <div
          className="relative aspect-[2/3] w-full overflow-hidden rounded-[--radius-lg] bg-bg-elevated"
        >
          {item.poster_url ? (
            <img
              src={item.poster_url}
              alt={`${item.title} poster`}
              className="h-full w-full object-cover"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
              <span className="text-6xl font-bold text-text-muted">
                {item.title.charAt(0).toUpperCase()}
              </span>
            </div>
          )}
        </div>

        <div className="flex flex-col gap-4">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-3xl font-bold text-text-primary sm:text-4xl">
                {item.title}
              </h1>
              <span className="rounded bg-bg-base px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-text-muted">
                {item.type}
              </span>
            </div>
            {(item.year || peer) && (
              <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-text-muted">
                {item.year && <span>{item.year}</span>}
                {item.year && peer && (
                  <span className="text-text-muted/40">·</span>
                )}
                {peer && (
                  <span className="inline-flex items-center gap-1.5">
                    <span
                      className="h-1.5 w-1.5 rounded-full bg-emerald-500"
                      aria-hidden
                    />
                    {t("peers.sharedBy", { name: peer.name })}
                  </span>
                )}
              </div>
            )}
          </div>

          {item.overview && (
            <p className="max-w-prose text-sm leading-relaxed text-text-secondary">
              {item.overview}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-3 pt-2">
            {resumeSeconds > 0 ? (
              <>
                <Button onClick={handleResume} disabled={showPlayer}>
                  ▶ {t("peers.resume", {
                    time: formatHms(resumeSeconds),
                    defaultValue: "Resume {{time}}",
                  })}
                </Button>
                <Button
                  variant="secondary"
                  onClick={handlePlay}
                  disabled={showPlayer}
                >
                  {t("peers.playFromStart", { defaultValue: "Play from start" })}
                </Button>
              </>
            ) : (
              <Button onClick={handlePlay} disabled={showPlayer}>
                ▶ {t("peers.play")}
              </Button>
            )}
            <Button
              variant="secondary"
              onClick={() => navigate(backLink)}
            >
              {t("peers.backToLibrary")}
            </Button>
          </div>

          {playError && (
            <p
              role="alert"
              className="rounded border border-danger/40 bg-danger/5 p-3 text-sm text-danger"
            >
              {playError}
            </p>
          )}
        </div>
      </div>

      {showPlayer && playerInfo && (
        <VideoPlayer
          itemId={itemId}
          peerId={peerId}
          sessionToken=""
          masterPlaylistUrl={playerInfo.masterUrl}
          directUrl={null}
          playbackMethod={playerInfo.method}
          startPosition={playerInfo.startPosition}
          title={item.title}
          onClose={handleClosePlayer}
        />
      )}
    </div>
  );
}

function formatHms(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, "0")}:${sec.toString().padStart(2, "0")}`;
  }
  return `${m}:${sec.toString().padStart(2, "0")}`;
}
