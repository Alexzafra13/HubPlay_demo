import { useState, useRef, useEffect, useCallback } from "react";
import type { FC, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { Button } from "@/components/common/Button";
import { Badge } from "@/components/common/Badge";

// ─── Menu item type ─────────────────────────────────────────────────────────

export interface HeroMenuItem {
  label: string;
  icon: ReactNode;
  onClick: () => void;
  variant?: "default" | "danger";
  adminOnly?: boolean;
}

interface HeroSectionProps {
  item: MediaItem;
  onPlay?: () => void;
  onToggleFavorite?: () => void;
  isFavorite?: boolean;
  menuItems?: HeroMenuItem[];
}

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

function formatRuntime(ticks: number | null | undefined): string | null {
  if (!ticks) return null;
  const totalMin = Math.round(ticks / 10_000_000 / 60);
  if (totalMin < 60) return `${totalMin}m`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

// ─── Kebab menu ─────────────────────────────────────────────────────────────

const KebabMenu: FC<{ items: HeroMenuItem[] }> = ({ items }) => {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => setOpen(false), []);

  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        close();
      }
    };
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("mousedown", onClickOutside);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onClickOutside);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open, close]);

  if (items.length === 0) return null;

  return (
    <div ref={menuRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
        aria-label="More options"
        aria-expanded={open}
      >
        <svg className="h-5 w-5 text-text-secondary" viewBox="0 0 24 24" fill="currentColor">
          <circle cx="12" cy="5" r="1.5" />
          <circle cx="12" cy="12" r="1.5" />
          <circle cx="12" cy="19" r="1.5" />
        </svg>
      </button>

      {open && (
        <div className="absolute bottom-full mb-2 right-0 z-50 min-w-[200px] rounded-[--radius-lg] border border-border bg-bg-card shadow-xl shadow-black/40 backdrop-blur-xl overflow-hidden">
          {items.map((item, i) => (
            <button
              key={i}
              type="button"
              onClick={() => {
                close();
                item.onClick();
              }}
              className={[
                "flex w-full items-center gap-3 px-4 py-2.5 text-sm transition-colors cursor-pointer",
                item.variant === "danger"
                  ? "text-error hover:bg-error/10"
                  : "text-text-secondary hover:text-text-primary hover:bg-bg-elevated",
              ].join(" ")}
            >
              <span className="flex h-5 w-5 shrink-0 items-center justify-center">
                {item.icon}
              </span>
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

// ─── Hero section ───────────────────────────────────────────────────────────

const HeroSection: FC<HeroSectionProps> = ({
  item,
  onPlay,
  onToggleFavorite,
  isFavorite = false,
  menuItems = [],
}) => {
  const { t } = useTranslation();
  const duration = formatRuntime(item.runtime_ticks);

  return (
    <section className="relative flex w-full items-end overflow-hidden min-h-[55vh] sm:min-h-[60vh] lg:min-h-[70vh]">
      {/* Backdrop image */}
      {item.backdrop_url ? (
        <img
          src={item.backdrop_url}
          alt=""
          loading="eager"
          className="absolute inset-0 h-full w-full object-cover"
        />
      ) : item.poster_url ? (
        <img
          src={item.poster_url}
          alt=""
          loading="eager"
          className="absolute inset-0 h-full w-full object-cover blur-2xl scale-110"
        />
      ) : (
        <div className="absolute inset-0 bg-gradient-to-br from-bg-elevated to-bg-card" />
      )}

      {/* Gradient overlays — bottom fade + left vignette */}
      <div className="absolute inset-0 bg-gradient-to-t from-bg-base via-bg-base/70 to-transparent" />
      <div className="absolute inset-0 bg-gradient-to-r from-bg-base/80 via-transparent to-transparent" />

      {/* Content */}
      <div className="relative z-10 flex w-full items-end gap-6 px-6 pb-8 pt-32 sm:px-10 sm:pb-10 lg:gap-8 lg:pb-12">
        {/* Poster — hidden on mobile, visible from md up */}
        {item.poster_url && (
          <div className="hidden md:block shrink-0">
            <img
              src={item.poster_url}
              alt={item.title}
              className="h-[280px] lg:h-[340px] w-auto rounded-[--radius-lg] shadow-2xl shadow-black/50 object-cover"
            />
          </div>
        )}

        {/* Info column */}
        <div className="flex flex-1 flex-col gap-3 sm:gap-4 min-w-0">
          {/* Logo or title */}
          {item.logo_url ? (
            <img
              src={item.logo_url}
              alt={item.title}
              className="max-h-[60px] sm:max-h-[80px] lg:max-h-[100px] w-auto max-w-[70%] object-contain object-left drop-shadow-lg"
            />
          ) : (
            <h1 className="text-3xl font-bold text-text-primary sm:text-4xl lg:text-5xl drop-shadow-lg">
              {item.title}
            </h1>
          )}

          {/* Meta row */}
          <div className="flex flex-wrap items-center gap-2 sm:gap-3 text-sm text-text-secondary">
            {item.year != null && (
              <span className="font-medium">{item.year}</span>
            )}

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

            {duration && (
              <span className="text-text-muted">{duration}</span>
            )}

            {item.genres?.map((genre) => (
              <Badge key={genre}>{genre}</Badge>
            ))}
          </div>

          {/* Overview — larger on desktop */}
          {item.overview != null && (
            <p className="max-w-2xl text-sm leading-relaxed text-text-secondary line-clamp-2 sm:line-clamp-3 sm:text-[15px]">
              {item.overview}
            </p>
          )}

          {/* Action buttons */}
          <div className="flex items-center gap-3 pt-1 sm:pt-2">
            <Button size="lg" onClick={onPlay}>
              <svg
                className="h-5 w-5"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path d="M8 5v14l11-7z" />
              </svg>
              {t("common.play")}
            </Button>

            <button
              type="button"
              onClick={onToggleFavorite}
              className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
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

            {menuItems.length > 0 && <KebabMenu items={menuItems} />}
          </div>
        </div>
      </div>
    </section>
  );
};

export { HeroSection };
export type { HeroSectionProps, HeroMenuItem };
