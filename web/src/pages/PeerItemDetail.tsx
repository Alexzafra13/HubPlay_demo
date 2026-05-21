import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import {
  useMyPeers,
  usePeerItems,
  usePeerLibraries,
} from "@/api/hooks/federation";
import { Spinner, EmptyState } from "@/components/common";
import { HeroSection, type HeroMenuItem } from "@/components/media/HeroSection";
import { VideoPlayer } from "@/components/player";
import { federationItemToMediaItem } from "@/api/federationAdapter";
import { useVibrantColors } from "@/hooks/useVibrantColors";
import { buildAuroraStyle } from "./itemDetail/aurora";
import type {
  FederationRemoteItem,
  FederationRemoteItemsResponse,
  MediaItem,
  PeerItemProgress,
  PlaybackMethod,
} from "@/api/types";

const TICKS_PER_SECOND = 10_000_000;

// PeerItemDetail — Plex-style detail page for an item that lives on
// a federated peer.
//
// Renders the SAME `HeroSection` used by the local movie / season /
// episode detail page so the surface reads consistently regardless
// of whether the item is local or shared. The federation wire shape
// (id, type, title, year, overview, poster_url) is narrower than the
// local item shape, but the hero degrades gracefully:
//   - no backdrop_url   → falls back to poster_url
//   - no logo_url       → falls back to <h1>title</h1>
//   - no genres/rating  → those badge slots simply don't render
//
// The runtime vibrant-colour extraction (useVibrantColors) drives
// both the hero gradient (inside HeroSection) and the page-wide
// aurora canvas, so the page picks up the same warmth-of-the-poster
// feel a local detail surface gets from its server-precomputed
// palette.
//
// Peer attribution lives in the `studio` slot — that's the soft
// "· {studio}" attribution the local hero already shows after the
// taxonomy badges. Reads as "this came from Pedro's HubPlay" without
// adding a new slot.
//
// Resume UX: when there's a saved cross-peer position (federation
// progress migration 028), the primary CTA becomes "Reanudar 0:58"
// and "Reproducir desde el inicio" appears in the kebab menu. Same
// affordance pattern Plex uses for cross-device resume.
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
  // pair y devuelve el primer match. Map en lugar de find anidado en
  // loop — react-doctor/js-index-maps lo prefiere y mantiene O(N+M)
  // sin saltarse páginas.
  const item = useMemo<FederationRemoteItem | undefined>(() => {
    const cached = queryClient.getQueriesData<FederationRemoteItemsResponse>({
      queryKey: ["me", "peers", peerId, "libraries", libraryId, "items"],
    });
    const byId = new Map<string, FederationRemoteItem>();
    for (const [, value] of cached) {
      if (!value) continue;
      for (const it of value.items) byId.set(it.id, it);
    }
    for (const it of items.data?.items ?? []) byId.set(it.id, it);
    return byId.get(itemId);
  }, [queryClient, peerId, libraryId, itemId, items.data]);

  // ─── Player overlay state ────────────────────────────────────────

  const [showPlayer, setShowPlayer] = useState(false);
  const [playerInfo, setPlayerInfo] = useState<{
    masterUrl: string;
    method: PlaybackMethod;
    startPosition: number;
    peerSessionId: string;
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
          peerSessionId: resp.peer_session_id,
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
    if (peerId && itemId) {
      api.getPeerItemProgress(peerId, itemId).then(setProgress).catch(() => {});
    }
  }, [peerId, itemId]);

  // ─── Adapt to MediaItem so HeroSection consumes it directly ──────

  // We mutate the adapted shape with the peer's name in the `studio`
  // slot (soft attribution after the taxonomy chips) and patch the
  // backdrop fallback so HeroSection's gradient + bottom fade have
  // a richer image to work with than the cropped poster.
  const mediaItem = useMemo<MediaItem | null>(() => {
    if (!item) return null;
    const base = federationItemToMediaItem(item);
    return {
      ...base,
      studio: peer?.name,
    };
  }, [item, peer?.name]);

  // Aurora palette — prefer the peer's pre-extracted swatches (same
  // path the local detail page uses for items in our catalog). When
  // backdrop_colors lands on the wire we skip node-vibrant entirely;
  // the runtime extractor only runs as a fallback for older peers
  // that pre-date the federation-side colour plumbing or items whose
  // primary image hasn't been extracted yet.
  //
  // The fallback URL is gated on hasServerPalette so the dynamic
  // import of node-vibrant doesn't load when the server already
  // delivered a palette — same first-paint cost as a local item.
  const hasServerPalette = !!(
    mediaItem?.backdrop_colors?.vibrant || mediaItem?.backdrop_colors?.muted
  );
  const fallbackUrl = hasServerPalette ? null : mediaItem?.poster_url ?? null;
  const runtimePalette = useVibrantColors(fallbackUrl);
  const aurora = useMemo(
    () =>
      buildAuroraStyle({
        vibrant:
          mediaItem?.backdrop_colors?.vibrant ?? runtimePalette.vibrant ?? undefined,
        muted:
          mediaItem?.backdrop_colors?.muted ?? runtimePalette.muted ?? undefined,
        darkVibrant: runtimePalette.darkVibrant ?? undefined,
        lightVibrant: runtimePalette.lightVibrant ?? undefined,
        lightMuted: runtimePalette.lightMuted ?? undefined,
      }),
    [
      mediaItem?.backdrop_colors?.vibrant,
      mediaItem?.backdrop_colors?.muted,
      runtimePalette.vibrant,
      runtimePalette.muted,
      runtimePalette.darkVibrant,
      runtimePalette.lightVibrant,
      runtimePalette.lightMuted,
    ],
  );

  // ─── Hero menu rows ──────────────────────────────────────────────

  const backLink = `/peers/${peerId}/libraries/${libraryId}`;
  const menuItems = useMemo<HeroMenuItem[]>(() => {
    const rows: HeroMenuItem[] = [];
    if (resumeSeconds > 0) {
      rows.push({
        label: t("peers.playFromStart"),
        icon: <PlayFromStartIcon />,
        onClick: handlePlay,
      });
    }
    rows.push({
      label: t("peers.backToLibrary"),
      icon: <BackIcon />,
      onClick: () => navigate(backLink),
    });
    return rows;
  }, [resumeSeconds, t, handlePlay, navigate, backLink]);

  // ─── Render ──────────────────────────────────────────────────────

  if (items.isLoading && !item) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!item || !mediaItem) {
    return (
      <div className="p-6 sm:p-10">
        <EmptyState
          title={t("peers.itemNotFoundTitle")}
          description={t("peers.itemNotFoundDescription")}
        />
      </div>
    );
  }

  const playLabel =
    resumeSeconds > 0
      ? t("peers.resume", { time: formatHms(resumeSeconds) })
      : t("peers.play");

  // Aurora applied directly to the wrapper — same reasoning as
  // ItemDetail.tsx: the fixed/-z-10 canvas lost a stacking battle
  // against the body's bg-base propagation in some browsers and
  // stayed invisible. Painting on the wrapper is unambiguous.
  const wrapperStyle = aurora.auroraBackground
    ? { ...aurora.detailStyle, ...aurora.auroraBackground }
    : aurora.detailStyle;

  return (
    <div
      className="flex flex-col min-h-screen -mx-4 md:-mx-6 -mb-4 md:-mb-6"
      style={wrapperStyle}
    >
      {/* No separate aurora canvas — the wrapper carries the colour
          itself. --detail-tint is still published so the hero's
          bottom-fade gradient lands on the same swatch. */}

      {showPlayer && playerInfo && (
        <VideoPlayer
          itemId={itemId}
          peerId={peerId}
          peerStreamSessionId={playerInfo.peerSessionId}
          sessionToken=""
          masterPlaylistUrl={playerInfo.masterUrl}
          directUrl={null}
          playbackMethod={playerInfo.method}
          startPosition={playerInfo.startPosition}
          title={item.title}
          // Federated items don't carry a backdrop in the wire shape
          // (slim by design — see federationItemToMediaItem), so the
          // loading overlay falls back to the poster. Still way more
          // cinematic than a bare black <video> while the cross-peer
          // stream session warms up.
          backdropUrl={item.poster_url ?? undefined}
          onClose={handleClosePlayer}
        />
      )}

      <HeroSection
        item={mediaItem}
        onPlay={resumeSeconds > 0 ? handleResume : handlePlay}
        playLabel={playLabel}
        menuItems={menuItems}
      />

      {playError && (
        <div className="mx-6 mt-4 rounded-[--radius-md] bg-error/10 px-4 py-3 text-sm text-error sm:mx-10">
          {playError}
        </div>
      )}

      {/* Below-the-fold attribution row. The hero's chip strip already
          shows the peer name as `· {studio}`, but the explicit
          "shared by Pedro" pill with the live emerald dot reads more
          like a Plex/Jellyfin "this server is online" affordance. */}
      <div className="px-6 pt-8 sm:px-10">
        <div className="flex flex-wrap items-center gap-3 text-sm text-text-muted">
          {peer && (
            <span className="inline-flex items-center gap-2 rounded-full border border-border bg-bg-card/60 px-3 py-1.5 backdrop-blur-sm">
              <span
                className="size-2 rounded-full bg-emerald-500"
                aria-hidden
              />
              {t("peers.sharedBy", { name: peer.name })}
            </span>
          )}
          {library?.name && (
            <span className="inline-flex items-center gap-2">
              <span aria-hidden className="text-text-muted/40">·</span>
              <span>{library.name}</span>
            </span>
          )}
        </div>
      </div>
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

function PlayFromStartIcon() {
  return (
    <svg className="size-4" viewBox="0 0 24 24" fill="currentColor">
      <path d="M6 6h2v12H6zM10 12l9-6v12z" />
    </svg>
  );
}

function BackIcon() {
  return (
    <svg
      className="size-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M19 12H5M12 19l-7-7 7-7" />
    </svg>
  );
}
