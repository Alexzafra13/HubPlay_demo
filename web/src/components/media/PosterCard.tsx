import { memo } from "react";
import { Link } from "react-router";
import type { FC } from "react";
import type { MediaItem } from "@/api/types";

interface PosterCardProps {
  item: MediaItem;
  progress?: number;
  onClick?: () => void;
}

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

const PosterCard: FC<PosterCardProps> = memo(({ item, progress, onClick }) => {
  const href = item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;

  return (
    <Link
      to={href}
      onClick={onClick}
      className="group relative flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card rounded-[--radius-lg]"
    >
      {/* Poster image */}
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated transition-all duration-300 group-hover:scale-[1.03] group-hover:shadow-lg group-hover:shadow-accent/10">
        {item.poster_url ? (
          <img
            src={item.poster_url}
            alt={`${item.title} poster`}
            loading="lazy"
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-4xl font-bold text-text-muted">
              {item.title.charAt(0).toUpperCase()}
            </span>
          </div>
        )}

        {/* Hover overlay */}
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/60 opacity-0 transition-opacity duration-300 group-hover:opacity-100">
          {/* Play button */}
          <div className="flex h-12 w-12 items-center justify-center rounded-full border-2 border-white bg-white/10 backdrop-blur-sm">
            <svg
              className="h-5 w-5 text-white"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
          <p className="px-3 text-center text-sm font-semibold text-white">
            {item.title}
          </p>
          {item.year != null && (
            <p className="text-xs text-white/70">{item.year}</p>
          )}
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

      {/* Info below poster */}
      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="truncate text-sm font-medium text-text-primary">
          {item.title}
        </p>
        <div className="flex items-center gap-2 text-xs text-text-muted">
          {item.year != null && <span>{item.year}</span>}
          {item.community_rating != null && (
            <span className="flex items-center gap-0.5">
              <svg
                className="h-3 w-3 text-warning"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
              </svg>
              {formatRating(item.community_rating)}
            </span>
          )}
        </div>
      </div>
    </Link>
  );
});

PosterCard.displayName = "PosterCard";

export { PosterCard };
export type { PosterCardProps };
