import { useState, useCallback, useRef, useEffect, useMemo } from "react";
import { useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useItem, useItemChildren, useToggleFavorite, queryKeys } from "@/api/hooks";
import { api } from "@/api/client";
import type { MediaItem, PlaybackMethod } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";
import { HeroSection, MediaMeta, EpisodeCard } from "@/components/media";
import type { HeroMenuItem } from "@/components/media/HeroSection";
import { VideoPlayer } from "@/components/player";
import { ImageManager } from "@/components/ImageManager";
import { useAuthStore } from "@/store/auth";

export default function ItemDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const { data: item, isLoading, isError } = useItem(id ?? "");
  const user = useAuthStore((s) => s.user);
  const isAdmin = user?.role === "admin";

  const queryClient = useQueryClient();
  const toggleFavoriteMutation = useToggleFavorite();

  // Image manager state
  const [imageManagerOpen, setImageManagerOpen] = useState(false);

  // Player state
  const [showPlayer, setShowPlayer] = useState(false);
  const [playerInfo, setPlayerInfo] = useState<{
    playbackMethod: PlaybackMethod;
    masterPlaylistUrl: string | null;
    directUrl: string | null;
  } | null>(null);
  const [playError, setPlayError] = useState<string | null>(null);
  const isPlayingRef = useRef(false);

  // Episode context for prefetching next episode
  const [playingItemId, setPlayingItemId] = useState<string | null>(null);
  const [siblingEpisodes, setSiblingEpisodes] = useState<MediaItem[]>([]);

  // Fetch sibling episodes when the current item is an episode
  const parentId = item?.parent_id;
  const { data: siblings } = useItemChildren(parentId ?? "", { enabled: !!parentId && item?.type === "episode" });
  useEffect(() => {
    if (siblings && siblings.length > 0) {
      const episodes = siblings.filter((s) => s.type === "episode")
        .sort((a, b) => (a.episode_number ?? 0) - (b.episode_number ?? 0));
      setSiblingEpisodes(episodes);
    }
  }, [siblings]);

  // ─── Favorite state ─────────────────────────────────────────────────────

  const isFavorite = item?.user_data?.is_favorite ?? false;

  const handleToggleFavorite = useCallback(() => {
    if (!id) return;
    toggleFavoriteMutation.mutate(id);
  }, [id, toggleFavoriteMutation]);

  // ─── Kebab menu items ───────────────────────────────────────────────────

  const menuItems = useMemo<HeroMenuItem[]>(() => {
    const items: HeroMenuItem[] = [];

    // Admin-only items
    if (isAdmin && id) {
      items.push({
        label: t("imageManager.title"),
        icon: (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 15.75l5.159-5.159a2.25 2.25 0 013.182 0l5.159 5.159m-1.5-1.5l1.409-1.409a2.25 2.25 0 013.182 0l2.909 2.909M3.75 21h16.5A2.25 2.25 0 0022.5 18.75V5.25A2.25 2.25 0 0020.25 3H3.75A2.25 2.25 0 001.5 5.25v13.5A2.25 2.25 0 003.75 21z" />
          </svg>
        ),
        onClick: () => setImageManagerOpen(true),
      });

      items.push({
        label: t("itemDetail.refreshMetadata"),
        icon: (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182" />
          </svg>
        ),
        onClick: () => {
          // Re-fetch this item's metadata
          queryClient.invalidateQueries({ queryKey: queryKeys.item(id!) });
        },
      });
    }

    // Media info (scroll to section)
    if (item?.media_streams && item.media_streams.length > 0) {
      items.push({
        label: t("itemDetail.mediaInfo"),
        icon: (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" />
          </svg>
        ),
        onClick: () => {
          document.getElementById("media-info-section")?.scrollIntoView({ behavior: "smooth" });
        },
      });
    }

    return items;
  }, [isAdmin, id, item, t]);

  // ─── Playback ───────────────────────────────────────────────────────────

  const cleanupSession = useCallback(async (itemId: string) => {
    try {
      const token = localStorage.getItem("hubplay_access_token");
      await fetch(`/api/v1/stream/${itemId}/session`, {
        method: "DELETE",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
    } catch { /* best-effort cleanup */ }
    isPlayingRef.current = false;
  }, []);

  const handlePlay = useCallback(async () => {
    if (!id) return;
    setPlayError(null);

    try {
      if (isPlayingRef.current) {
        await cleanupSession(id);
      }

      const info = await api.getStreamInfo(id);
      const rawMethod = (info as Record<string, unknown>).method as string ?? "";
      const methodMap: Record<string, PlaybackMethod> = {
        DirectPlay: "direct_play",
        DirectStream: "direct_stream",
        Transcode: "transcode",
      };
      const method: PlaybackMethod = methodMap[rawMethod] ?? "transcode";

      const masterUrl = method !== "direct_play"
        ? `/api/v1/stream/${id}/master.m3u8`
        : null;
      const directUrl = method === "direct_play"
        ? `/api/v1/stream/${id}/direct`
        : null;

      isPlayingRef.current = true;
      setPlayingItemId(id);
      setPlayerInfo({ playbackMethod: method, masterPlaylistUrl: masterUrl, directUrl });
      setShowPlayer(true);
    } catch {
      setPlayError(t('itemDetail.playbackError'));
    }
  }, [id, cleanupSession, t]);

  // Prefetch next episode's item data when an episode starts playing
  useEffect(() => {
    if (!playingItemId || siblingEpisodes.length === 0) return;
    const idx = siblingEpisodes.findIndex((ep) => ep.id === playingItemId);
    const nextEp = idx >= 0 ? siblingEpisodes[idx + 1] : undefined;
    if (nextEp) {
      queryClient.prefetchQuery({
        queryKey: queryKeys.item(nextEp.id),
        queryFn: () => api.getItem(nextEp.id),
        staleTime: 5 * 60 * 1000,
      });
    }
  }, [playingItemId, siblingEpisodes, queryClient]);

  const handlePlayerEnded = useCallback(() => {
    if (!playingItemId || siblingEpisodes.length === 0) return;
    const idx = siblingEpisodes.findIndex((ep) => ep.id === playingItemId);
    const nextEp = idx >= 0 ? siblingEpisodes[idx + 1] : undefined;
    if (!nextEp) return;

    setPlayingItemId(nextEp.id);
    (async () => {
      try {
        if (isPlayingRef.current && playingItemId) {
          await cleanupSession(playingItemId);
        }
        const info = await api.getStreamInfo(nextEp.id);
        const rawMethod = (info as Record<string, unknown>).method as string ?? "";
        const methodMap: Record<string, PlaybackMethod> = {
          DirectPlay: "direct_play", DirectStream: "direct_stream", Transcode: "transcode",
        };
        const method: PlaybackMethod = methodMap[rawMethod] ?? "transcode";
        isPlayingRef.current = true;
        setPlayerInfo({
          playbackMethod: method,
          masterPlaylistUrl: method !== "direct_play" ? `/api/v1/stream/${nextEp.id}/master.m3u8` : null,
          directUrl: method === "direct_play" ? `/api/v1/stream/${nextEp.id}/direct` : null,
        });
      } catch {
        setShowPlayer(false);
        setPlayerInfo(null);
      }
    })();
  }, [playingItemId, siblingEpisodes, cleanupSession]);

  const handleClosePlayer = useCallback(async () => {
    setShowPlayer(false);
    setPlayerInfo(null);
    setPlayingItemId(null);
    if (playingItemId || id) {
      await cleanupSession(playingItemId || id!);
    }
  }, [id, playingItemId, cleanupSession]);

  // ─── Render ─────────────────────────────────────────────────────────────

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (isError || !item) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <EmptyState
          title={t('itemDetail.notFoundTitle')}
          description={t('itemDetail.notFoundDescription')}
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z"
              />
            </svg>
          }
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      {/* Video Player Overlay */}
      {showPlayer && playerInfo && (playingItemId || id) && (
        <VideoPlayer
          itemId={playingItemId || id!}
          sessionToken=""
          masterPlaylistUrl={playerInfo.masterPlaylistUrl}
          directUrl={playerInfo.directUrl}
          playbackMethod={playerInfo.playbackMethod}
          title={item.title}
          knownDuration={
            item.duration_ticks
              ? item.duration_ticks / 10_000_000
              : undefined
          }
          onClose={handleClosePlayer}
          onEnded={handlePlayerEnded}
        />
      )}

      <HeroSection
        item={item}
        onPlay={handlePlay}
        onToggleFavorite={handleToggleFavorite}
        isFavorite={isFavorite}
        menuItems={menuItems}
      />

      {playError && (
        <div className="mx-6 mt-4 rounded-[--radius-md] bg-error/10 px-4 py-3 text-sm text-error sm:mx-10">
          {playError}
        </div>
      )}

      <div className="flex flex-col gap-8 px-6 py-8 sm:px-10">
        {/* Media info */}
        {item.media_streams?.length > 0 && (
          <section id="media-info-section">
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.mediaInfo')}
            </h2>
            <MediaMeta streams={item.media_streams} />
          </section>
        )}

        {/* Cast */}
        {item.people?.length > 0 && (
          <section>
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.cast')}
            </h2>
            <div className="flex flex-wrap gap-3">
              {item.people.slice(0, 12).map((person) => (
                <div
                  key={`${person.name}-${person.role}`}
                  className="flex items-center gap-2 rounded-[--radius-md] bg-bg-elevated px-3 py-2"
                >
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-bg-card text-xs font-bold text-text-muted">
                    {person.name.charAt(0)}
                  </div>
                  <div className="flex flex-col">
                    <span className="text-sm font-medium text-text-primary">
                      {person.name}
                    </span>
                    {person.role && (
                      <span className="text-xs text-text-muted">
                        {person.role}
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Seasons & Episodes (for series) */}
        {item.type === "series" && <SeasonEpisodes seriesId={item.id} />}
      </div>

      {/* Image Manager (admin only) */}
      {isAdmin && id && (
        <ImageManager
          itemId={id}
          isOpen={imageManagerOpen}
          onClose={() => setImageManagerOpen(false)}
        />
      )}
    </div>
  );
}

function SeasonEpisodes({ seriesId }: { seriesId: string }) {
  const { t } = useTranslation();
  const { data: children, isLoading } = useItemChildren(seriesId);

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner size="md" />
      </div>
    );
  }

  if (!children || children.length === 0) return null;

  const seasons = children.filter((c) => c.type === "season");
  const episodes = children.filter((c) => c.type === "episode");

  if (seasons.length > 0) {
    return <SeasonTabs seasons={seasons} />;
  }

  return (
    <section>
      <h2 className="mb-4 text-lg font-semibold text-text-primary">
        {t('itemDetail.episodes')}
      </h2>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
        {episodes.map((ep) => (
          <EpisodeCard key={ep.id} item={ep} />
        ))}
      </div>
    </section>
  );
}

function SeasonTabs({ seasons }: { seasons: MediaItem[] }) {
  const { t } = useTranslation();
  const sorted = [...seasons].sort(
    (a, b) => (a.season_number ?? 0) - (b.season_number ?? 0),
  );
  const [activeSeason, setActiveSeason] = useState(sorted[0]?.id ?? "");
  const { data: episodes, isLoading } = useItemChildren(activeSeason);

  return (
    <section>
      <h2 className="mb-4 text-lg font-semibold text-text-primary">
        {t('itemDetail.seasons')}
      </h2>

      <div className="mb-6 flex gap-2 overflow-x-auto pb-2">
        {sorted.map((season) => (
          <button
            key={season.id}
            type="button"
            onClick={() => setActiveSeason(season.id)}
            className={[
              "shrink-0 rounded-[--radius-md] px-4 py-2 text-sm font-medium transition-colors",
              activeSeason === season.id
                ? "bg-accent text-white"
                : "bg-bg-elevated text-text-secondary hover:text-text-primary hover:bg-bg-card",
            ].join(" ")}
          >
            {season.title}
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="flex justify-center py-8">
          <Spinner size="md" />
        </div>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
          {(episodes ?? []).map((ep) => (
            <EpisodeCard key={ep.id} item={ep} />
          ))}
        </div>
      )}
    </section>
  );
}
