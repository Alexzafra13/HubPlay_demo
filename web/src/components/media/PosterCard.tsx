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
      className="group flex flex-col outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card rounded-[--radius-lg]"
    >
      {/* Poster image */}
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated transition-transform duration-300 group-hover:scale-[1.03]">
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

        {/* Hover: subtle play icon, no text overlay */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors duration-200 group-hover:bg-black/30">
          <div className="flex h-11 w-11 items-center justify-center rounded-full bg-white/20 backdrop-blur-sm opacity-0 scale-90 transition-all duration-200 group-hover:opacity-100 group-hover:scale-100">
            <svg
              className="h-5 w-5 text-white ml-0.5"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
        </div>

        {/* Rating badge — top right */}
        {item.community_rating != null && (
          <div className="absolute top-2 right-2 flex items-center gap-1 rounded-[--radius-sm] bg-black/70 backdrop-blur-sm px-1.5 py-0.5 text-[11px] font-semibold text-white">
            <svg className="h-2.5 w-2.5 text-warning" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
            </svg>
            {formatRating(item.community_rating)}
          </div>
        )}

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

      {/* Info below — clean, no overlap */}
      <div className="flex flex-col gap-0.5 pt-2 px-0.5">
        <p className="truncate text-sm font-medium text-text-primary group-hover:text-white transition-colors">
          {item.title}
        </p>
        <div className="flex items-center gap-1.5 text-xs text-text-muted">
          {item.year != null && <span>{item.year}</span>}
          {item.genres?.length > 0 && (
            <>
              {item.year != null && <span className="text-text-muted/40">·</span>}
              <span className="truncate">{item.genres.slice(0, 2).join(", ")}</span>
            </>
          )}
        </div>
      </div>
    </Link>
  );
});

PosterCard.displayName = "PosterCard";

export { PosterCard };
export type { PosterCardProps };
