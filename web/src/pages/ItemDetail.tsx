import { useState, useCallback, useRef } from "react";
import { useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useItem, useItemChildren } from "@/api/hooks";
import { api } from "@/api/client";
import type { MediaItem, PlaybackMethod } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";
import { HeroSection, MediaMeta, EpisodeCard } from "@/components/media";
import { VideoPlayer } from "@/components/player";

export default function ItemDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const { data: item, isLoading, isError } = useItem(id ?? "");

  // Player state
  const [showPlayer, setShowPlayer] = useState(false);
  const [playerInfo, setPlayerInfo] = useState<{
    playbackMethod: PlaybackMethod;
    masterPlaylistUrl: string | null;
    directUrl: string | null;
  } | null>(null);
  const [playError, setPlayError] = useState<string | null>(null);
  const isPlayingRef = useRef(false);

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
      // Clean up any existing session before starting a new one
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
      setPlayerInfo({ playbackMethod: method, masterPlaylistUrl: masterUrl, directUrl });
      setShowPlayer(true);
    } catch {
      setPlayError(t('itemDetail.playbackError'));
    }
  }, [id, cleanupSession]);

  const handleClosePlayer = useCallback(async () => {
    setShowPlayer(false);
    setPlayerInfo(null);
    if (id) {
      await cleanupSession(id);
    }
  }, [id, cleanupSession]);

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
      {showPlayer && playerInfo && id && (
        <VideoPlayer
          itemId={id}
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
        />
      )}

      <HeroSection item={item} onPlay={handlePlay} />

      {playError && (
        <div className="mx-6 mt-4 rounded-[--radius-md] bg-error/10 px-4 py-3 text-sm text-error sm:mx-10">
          {playError}
        </div>
      )}

      <div className="flex flex-col gap-8 px-6 py-8 sm:px-10">
        {/* Overview */}
        {item.overview && (
          <section>
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.overview')}
            </h2>
            <p className="max-w-3xl leading-relaxed text-text-secondary">
              {item.overview}
            </p>
          </section>
        )}

        {/* Media info */}
        {item.media_streams?.length > 0 && (
          <section>
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

  // Separate seasons from episodes
  const seasons = children.filter((c) => c.type === "season");
  const episodes = children.filter((c) => c.type === "episode");

  // If we have seasons, show tabs. Otherwise show episodes directly.
  if (seasons.length > 0) {
    return <SeasonTabs seasons={seasons} />;
  }

  // Direct episodes (flat series)
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

      {/* Season tabs */}
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

      {/* Episodes for selected season */}
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
