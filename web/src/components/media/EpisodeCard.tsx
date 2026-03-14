import { Link } from "react-router";
import type { FC } from "react";
import type { MediaItem } from "@/api/types";

interface EpisodeCardProps {
  item: MediaItem;
  progress?: number;
  onClick?: () => void;
}

function formatEpisodeCode(season: number | null, episode: number | null): string {
  const s = String(season ?? 1).padStart(2, "0");
  const e = String(episode ?? 1).padStart(2, "0");
  return `S${s}E${e}`;
}

function formatDuration(ticks: number | null): string | null {
  if (ticks == null) return null;
  const minutes = Math.round(ticks / (10_000_000 * 60));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remaining = minutes % 60;
  return `${hours}h ${remaining}m`;
}

const EpisodeCard: FC<EpisodeCardProps> = ({ item, progress, onClick }) => {
  const episodeCode = formatEpisodeCode(item.season_number, item.episode_number);
  const duration = formatDuration(item.runtime_ticks);
  const href = `/episodes/${item.id}`;

  return (
    <Link
      to={href}
      onClick={onClick}
      className="group flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card rounded-[--radius-lg]"
    >
      {/* Thumbnail */}
      <div className="relative aspect-video overflow-hidden rounded-[--radius-lg] bg-bg-elevated transition-all duration-300 group-hover:shadow-lg group-hover:shadow-accent/10">
        {item.backdrop_url ?? item.poster_url ? (
          <img
            src={(item.backdrop_url ?? item.poster_url)!}
            alt={`${item.title} thumbnail`}
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-bg-card">
            <span className="text-lg font-bold text-text-muted">
              {episodeCode}
            </span>
          </div>
        )}

        {/* Hover play icon */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity duration-300 group-hover:opacity-100">
          <div className="flex h-10 w-10 items-center justify-center rounded-full border-2 border-white bg-white/10 backdrop-blur-sm">
            <svg
              className="h-4 w-4 text-white"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
        </div>

        {/* Progress bar */}
        {progress != null && progress > 0 && (
          <div className="absolute bottom-0 left-0 right-0 h-1 bg-black/40">
            <div
              className="h-full bg-accent transition-all duration-300"
              style={{ width: `${Math.min(100, Math.max(0, progress))}%` }}
            />
          </div>
        )}
      </div>

      {/* Info below thumbnail */}
      <div className="flex flex-col gap-1 px-0.5">
        <p className="truncate text-sm font-medium text-text-primary">
          {item.title}
        </p>
        <div className="flex items-center gap-2 text-xs text-text-muted">
          <span className="rounded-[--radius-sm] bg-bg-elevated px-1.5 py-0.5 font-medium text-text-secondary">
            {episodeCode}
          </span>
          {duration != null && <span>{duration}</span>}
        </div>
      </div>
    </Link>
  );
};

export { EpisodeCard };
export type { EpisodeCardProps };
