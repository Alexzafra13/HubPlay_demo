import { useState } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { Button } from "@/components/common/Button";
import { Badge } from "@/components/common/Badge";

interface HeroSectionProps {
  item: MediaItem;
  onPlay?: () => void;
}

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

const HeroSection: FC<HeroSectionProps> = ({ item, onPlay }) => {
  const [isFavorite, setIsFavorite] = useState(false);

  return (
    <section className="relative flex min-h-[60vh] w-full items-end overflow-hidden">
      {/* Backdrop image */}
      {item.backdrop_url ? (
        <img
          src={item.backdrop_url}
          alt={`${item.title} backdrop`}
          loading="lazy"
          className="absolute inset-0 h-full w-full object-cover"
        />
      ) : item.poster_url ? (
        <img
          src={item.poster_url}
          alt={`${item.title} backdrop`}
          loading="lazy"
          className="absolute inset-0 h-full w-full object-cover blur-2xl scale-110"
        />
      ) : (
        <div className="absolute inset-0 bg-gradient-to-br from-bg-elevated to-bg-card" />
      )}

      {/* Gradient overlay */}
      <div className="absolute inset-0 bg-gradient-to-t from-bg-base via-bg-base/60 to-transparent" />

      {/* Content */}
      <div className="relative z-10 flex w-full max-w-5xl flex-col gap-4 px-6 pb-10 pt-32 sm:px-10">
        {/* Title */}
        <h1 className="text-3xl font-bold text-text-primary sm:text-4xl lg:text-5xl">
          {item.title}
        </h1>

        {/* Meta row */}
        <div className="flex flex-wrap items-center gap-3 text-sm text-text-secondary">
          {item.year != null && <span>{item.year}</span>}

          {item.community_rating != null && (
            <Badge variant="warning">
              <svg
                className="h-3 w-3"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
              </svg>
              {formatRating(item.community_rating)}
            </Badge>
          )}

          {item.content_rating != null && (
            <Badge>{item.content_rating}</Badge>
          )}

          {item.genres?.map((genre) => (
            <Badge key={genre}>{genre}</Badge>
          ))}
        </div>

        {/* Overview */}
        {item.overview != null && (
          <p className="max-w-2xl text-sm leading-relaxed text-text-secondary line-clamp-2">
            {item.overview}
          </p>
        )}

        {/* Action buttons */}
        <div className="flex items-center gap-3 pt-2">
          <Button size="lg" onClick={onPlay}>
            <svg
              className="h-5 w-5"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
            Play
          </Button>

          <button
            type="button"
            onClick={() => setIsFavorite((f) => !f)}
            className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated"
            aria-label={isFavorite ? "Remove from favorites" : "Add to favorites"}
          >
            <svg
              className={`h-5 w-5 transition-colors ${isFavorite ? "text-error fill-error" : "text-text-secondary"}`}
              viewBox="0 0 24 24"
              fill={isFavorite ? "currentColor" : "none"}
              stroke="currentColor"
              strokeWidth={2}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M20.84 4.61a5.5 5.5 0 00-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 00-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 000-7.78z"
              />
            </svg>
          </button>
        </div>
      </div>
    </section>
  );
};

export { HeroSection };
export type { HeroSectionProps };
