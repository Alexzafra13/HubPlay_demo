import { useState, useEffect, useCallback } from "react";
import { Link, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useContinueWatching, useLatestItems, useNextUp } from "@/api/hooks";
import type { MediaItem } from "@/api/types";
import { Skeleton } from "@/components/common";
import { EpisodeCard } from "@/components/media";

// ─── Hero Banner ──────────────────────────────────────────────────────────────

const HERO_INTERVAL = 8000;

function HeroBanner({ items }: { items: MediaItem[] }) {
  const [activeIndex, setActiveIndex] = useState(0);
  const navigate = useNavigate();
  const { t } = useTranslation();

  const heroItems = items.filter((i) => i.backdrop_url).slice(0, 5);

  const goTo = useCallback((idx: number) => setActiveIndex(idx), []);

  useEffect(() => {
    if (heroItems.length <= 1) return;
    const timer = setInterval(() => {
      setActiveIndex((prev) => (prev + 1) % heroItems.length);
    }, HERO_INTERVAL);
    return () => clearInterval(timer);
  }, [heroItems.length]);

  if (heroItems.length === 0) return null;

  const item = heroItems[activeIndex] ?? heroItems[0];
  const href =
    item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;

  return (
    <section className="relative -mx-4 md:-mx-6 h-[70vh] min-h-[450px] max-h-[750px] overflow-hidden" style={{ marginTop: 'calc(var(--topbar-height) * -1)' }}>
      {/* Backdrop images */}
      {heroItems.map((hi, i) => (
        <img
          key={hi.id}
          src={hi.backdrop_url!}
          alt=""
          className={`absolute inset-0 h-full w-full object-cover transition-opacity duration-1000 ${
            i === activeIndex ? "opacity-100" : "opacity-0"
          }`}
        />
      ))}

      {/* Gradient overlays — cinematic fade */}
      <div className="absolute inset-0 bg-gradient-to-t from-bg-base from-5% via-bg-base/60 via-30% to-transparent to-70%" />
      <div className="absolute inset-0 bg-gradient-to-r from-bg-base/80 via-bg-base/20 via-50% to-transparent" />

      {/* Content */}
      <div className="absolute bottom-0 left-0 right-0 px-8 pb-12 md:px-12 md:pb-16">
        <div className="flex max-w-2xl flex-col gap-5">
          {/* Logo or Title */}
          {item.logo_url ? (
            <div className="flex flex-col gap-1">
              <img
                src={item.logo_url}
                alt={item.title}
                className="max-h-16 sm:max-h-20 lg:max-h-28 max-w-[70%] w-auto object-contain object-left drop-shadow-[0_2px_20px_rgba(0,0,0,0.8)]"
              />
              {/* Show year under logo since the title text is gone */}
              {item.year != null && (
                <span className="text-sm font-medium text-white/50 mt-1">{item.year}</span>
              )}
            </div>
          ) : (
            <h1 className="text-4xl font-extrabold tracking-tight text-white sm:text-5xl lg:text-6xl drop-shadow-[0_2px_20px_rgba(0,0,0,0.8)]">
              {item.title}
            </h1>
          )}

          {/* Meta row */}
          <div className="flex flex-wrap items-center gap-3 text-sm text-white/70">
            {!item.logo_url && item.year != null && !item.title.includes(String(item.year)) && (
              <span className="font-medium text-white/90">{item.year}</span>
            )}

            {item.community_rating != null && (
              <span className="flex items-center gap-1 text-white/90">
                <svg
                  className="h-3.5 w-3.5 text-warning"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                >
                  <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
                </svg>
                {item.community_rating.toFixed(1)}
              </span>
            )}

            {item.content_rating != null && (
              <span className="rounded border border-white/30 px-1.5 py-0.5 text-xs font-medium text-white/80">
                {item.content_rating}
              </span>
            )}

            {item.genres?.slice(0, 3).map((genre, i) => (
              <span key={genre} className="text-white/60">
                {i > 0 && <span className="mr-3">·</span>}
                {genre}
              </span>
            ))}
          </div>

          {/* Overview */}
          {item.overview != null && (
            <p className="max-w-xl text-sm leading-relaxed text-white/50 line-clamp-2">
              {item.overview}
            </p>
          )}

          {/* Action buttons */}
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => navigate(href)}
              className="flex items-center gap-2 rounded-lg bg-white px-7 py-3 text-sm font-bold text-black transition-all hover:bg-white/90 hover:scale-105 active:scale-95"
            >
              <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
                <path d="M8 5v14l11-7z" />
              </svg>
              {t('common.play')}
            </button>

            <Link
              to={href}
              className="flex items-center gap-2 rounded-lg bg-white/15 px-6 py-3 text-sm font-semibold text-white backdrop-blur-sm transition-all hover:bg-white/25"
            >
              <svg
                className="h-5 w-5"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
              >
                <circle cx="12" cy="12" r="10" />
                <line x1="12" y1="16" x2="12" y2="12" />
                <line x1="12" y1="8" x2="12.01" y2="8" />
              </svg>
              {t('common.moreInfo')}
            </Link>
          </div>

          {/* Dot indicators */}
          {heroItems.length > 1 && (
            <div className="flex items-center gap-2 pt-1">
              {heroItems.map((_, i) => (
                <button
                  key={i}
                  type="button"
                  onClick={() => goTo(i)}
                  aria-label={`Go to slide ${i + 1}`}
                  className={`h-1 rounded-full transition-all duration-300 ${
                    i === activeIndex
                      ? "w-8 bg-white"
                      : "w-4 bg-white/30 hover:bg-white/50"
                  }`}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

// ─── Landscape Card ───────────────────────────────────────────────────────────

function LandscapeCard({ item }: { item: MediaItem }) {
  const href =
    item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;
  const image = item.backdrop_url ?? item.poster_url;

  return (
    <Link
      to={href}
      className="group flex-shrink-0 w-[260px] sm:w-[300px] flex flex-col gap-2"
    >
      {/* Thumbnail */}
      <div className="relative aspect-[16/9] overflow-hidden rounded-[--radius-md] bg-bg-elevated">
        {image ? (
          <img
            src={image}
            alt={item.title}
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-2xl font-bold text-text-muted">
              {item.title.charAt(0)}
            </span>
          </div>
        )}

        {/* Hover play icon */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors duration-200 group-hover:bg-black/30">
          <div className="flex h-10 w-10 items-center justify-center rounded-full bg-white/20 backdrop-blur-sm opacity-0 scale-90 transition-all duration-200 group-hover:opacity-100 group-hover:scale-100">
            <svg className="h-5 w-5 text-white ml-0.5" viewBox="0 0 24 24" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
        </div>

        {/* Rating badge */}
        {item.community_rating != null && (
          <div className="absolute top-2 right-2 flex items-center gap-1 rounded-[--radius-sm] bg-black/70 backdrop-blur-sm px-1.5 py-0.5 text-[11px] font-semibold text-white">
            <svg className="h-2.5 w-2.5 text-warning" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
            </svg>
            {item.community_rating.toFixed(1)}
          </div>
        )}
      </div>

      {/* Info below — clean, readable */}
      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="text-sm font-medium text-text-primary truncate group-hover:text-white transition-colors">
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
}

// ─── Content Rows ─────────────────────────────────────────────────────────────

function ScrollRow({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex gap-4 overflow-x-auto pb-2 scrollbar-thin scrollbar-track-transparent scrollbar-thumb-white/10">
      {children}
    </div>
  );
}

function LandscapeSkeletonRow() {
  return (
    <div className="flex gap-4">
      {Array.from({ length: 5 }, (_, i) => (
        <div key={i} className="w-[280px] sm:w-[320px] shrink-0">
          <Skeleton
            variant="rectangular"
            className="aspect-[16/9] w-full rounded-md"
          />
        </div>
      ))}
    </div>
  );
}

function EpisodeSkeletonRow() {
  return (
    <div className="flex gap-4">
      {Array.from({ length: 4 }, (_, i) => (
        <div key={i} className="w-[280px] shrink-0">
          <Skeleton
            variant="rectangular"
            className="aspect-video w-full rounded-md"
          />
          <Skeleton variant="text" width="70%" className="mt-2" />
          <Skeleton variant="text" width="40%" className="mt-1" />
        </div>
      ))}
    </div>
  );
}

interface SectionProps {
  title: string;
  linkTo?: string;
  children: React.ReactNode;
}

function Section({ title, linkTo, children }: SectionProps) {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white">{title}</h2>
        {linkTo && (
          <Link
            to={linkTo}
            className="text-xs text-white/40 hover:text-white/70 transition-colors"
          >
            {t('common.seeAll')}
          </Link>
        )}
      </div>
      {children}
    </section>
  );
}

// ─── Home Page ────────────────────────────────────────────────────────────────

export default function Home() {
  const { t } = useTranslation();
  const continueWatching = useContinueWatching();
  const latestItems = useLatestItems();
  const nextUp = useNextUp();

  const continueItems = continueWatching.data ?? [];
  const latestList = latestItems.data ?? [];
  const nextUpList = nextUp.data ?? [];

  // Hero candidates: prefer continue watching, then latest items
  const heroItems = [...continueItems, ...latestList];

  const isLoading = continueWatching.isLoading && latestItems.isLoading;
  const hasError = continueWatching.isError || latestItems.isError || nextUp.isError;

  if (hasError && !isLoading && heroItems.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 py-32 text-center">
        <svg className="h-12 w-12 text-white/20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
          <path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
        </svg>
        <p className="text-white/50">{t('home.failedToLoad')}</p>
        <button
          type="button"
          onClick={() => { continueWatching.refetch(); latestItems.refetch(); nextUp.refetch(); }}
          className="rounded-lg bg-white/10 px-5 py-2 text-sm font-medium text-white hover:bg-white/20 transition-colors"
        >
          {t('common.retry')}
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-10 bg-bg-base min-h-screen -mx-4 -mb-4 md:-mx-6 md:-mb-6">
      {/* Hero Banner */}
      <div className="mx-4 md:mx-6">
        {isLoading ? (
          <div className="relative -mx-4 md:-mx-6 h-[70vh] min-h-[450px] max-h-[750px] bg-bg-base animate-pulse" style={{ marginTop: 'calc(var(--topbar-height) * -1)' }} />
        ) : (
          <HeroBanner items={heroItems} />
        )}
      </div>

      {/* Content rows */}
      <div className="flex flex-col gap-10 px-8 pb-12 md:px-12">
        {/* Continue Watching */}
        {(continueWatching.isLoading || continueItems.length > 0) && (
          <Section title={t('home.continueWatching')}>
            {continueWatching.isLoading ? (
              <LandscapeSkeletonRow />
            ) : (
              <ScrollRow>
                {continueItems.map((item: MediaItem) => (
                  <LandscapeCard key={item.id} item={item} />
                ))}
              </ScrollRow>
            )}
          </Section>
        )}

        {/* Recently Added */}
        {(latestItems.isLoading || latestList.length > 0) && (
          <Section title={t('home.recentlyAdded')}>
            {latestItems.isLoading ? (
              <LandscapeSkeletonRow />
            ) : (
              <ScrollRow>
                {latestList.map((item: MediaItem) => (
                  <LandscapeCard key={item.id} item={item} />
                ))}
              </ScrollRow>
            )}
          </Section>
        )}

        {/* Next Up */}
        {(nextUp.isLoading || nextUpList.length > 0) && (
          <Section title={t('home.nextUp')} linkTo="/series">
            {nextUp.isLoading ? (
              <EpisodeSkeletonRow />
            ) : (
              <ScrollRow>
                {nextUpList.map((item: MediaItem) => (
                  <div key={item.id} className="w-[280px] shrink-0">
                    <EpisodeCard item={item} />
                  </div>
                ))}
              </ScrollRow>
            )}
          </Section>
        )}
      </div>
    </div>
  );
}
