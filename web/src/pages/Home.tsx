// Home — the configurable home page.
//
// The shell is data-driven: the hero stays at the top (built from
// continue-watching + latest as candidates so the user always lands
// on something familiar), and the rails below are rendered from the
// user's saved home layout. The list of rails is fetched via
// /me/home/layout, which also tells us which library-scoped
// "Latest in <Library>" rails to mount and what title to show. New
// libraries are reconciled in server-side (always visible by
// default) so the home page never goes empty after a fresh install.
//
// Each rail owns its own data fetch + skeleton + empty-state, so
// this page only orchestrates ordering. Adding a new rail type later
// is a one-line case in `renderSection`.

import { useState, useEffect, useCallback, type ReactNode } from "react";
import { Link, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import {
  useContinueWatching,
  useHomeLayout,
  useLatestItems,
} from "@/api/hooks";
import type { HomeSection, MediaItem } from "@/api/types";
import {
  ContinueWatchingRail,
  LatestInLibraryRail,
  LiveNowRail,
  NextUpRail,
  PeerRecentRail,
  PeerContinueWatchingRail,
  TrendingRail,
} from "@/components/home";

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
  const detailHref =
    item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;
  // The Play CTA deep-links into the detail surface with ?play=1, which
  // ItemDetail picks up and forwards to handlePlay (movies) or to the
  // resume episode (series). One CTA, both paths.
  const playHref = `${detailHref}?play=1`;

  // Compact metadata row — Netflix-style separators, only the bits we
  // actually have. Keeps the hero from looking empty when overview is
  // missing and avoids stacking divs for absent fields.
  // Year is rendered under the logo when one exists (so we don't
  // duplicate it). When falling back to the textual title we surface
  // it here unless the title already carries it (e.g. "Movie (2025)").
  const metaParts: { key: string; node: ReactNode }[] = [];
  if (
    !item.logo_url &&
    item.year != null &&
    !item.title.includes(String(item.year))
  ) {
    metaParts.push({
      key: "year",
      node: <span className="font-medium text-white/90">{item.year}</span>,
    });
  }
  if (item.community_rating != null) {
    metaParts.push({
      key: "rating",
      node: (
        <span className="flex items-center gap-1 text-white/90">
          <svg className="h-3.5 w-3.5 text-warning" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
          </svg>
          {item.community_rating.toFixed(1)}
        </span>
      ),
    });
  }
  if (item.content_rating) {
    metaParts.push({
      key: "content",
      node: (
        <span className="rounded border border-white/30 px-1.5 py-0.5 text-[11px] font-medium text-white/80">
          {item.content_rating}
        </span>
      ),
    });
  }
  item.genres?.slice(0, 3).forEach((genre) =>
    metaParts.push({
      key: `genre-${genre}`,
      node: <span className="text-white/60">{genre}</span>,
    }),
  );

  return (
    <section
      className="relative -mx-4 md:-mx-6 h-[70vh] min-h-[450px] max-h-[750px] overflow-hidden"
      style={{ marginTop: "calc(var(--topbar-height) * -1)" }}
    >
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

      <div className="absolute inset-0 bg-gradient-to-t from-bg-base from-5% via-bg-base/60 via-30% to-transparent to-70%" />
      <div className="absolute inset-0 bg-gradient-to-r from-bg-base/80 via-bg-base/20 via-50% to-transparent" />

      <div className="absolute bottom-0 left-0 right-0 px-8 pb-12 md:px-12 md:pb-16">
        <div className="flex max-w-2xl flex-col gap-5">
          {/* Title block is the primary "more info" hit-target —
              clicking the logo / title navigates to the detail page,
              freeing the hero from a second action button. */}
          <Link to={detailHref} className="block w-fit">
            {item.logo_url ? (
              <div className="flex flex-col gap-1">
                <img
                  src={item.logo_url}
                  alt={item.title}
                  className="max-h-16 sm:max-h-20 lg:max-h-28 max-w-[70%] w-auto object-contain object-left drop-shadow-[0_2px_20px_rgba(0,0,0,0.8)]"
                />
                {item.year != null && (
                  <span className="text-sm font-medium text-white/50 mt-1">
                    {item.year}
                  </span>
                )}
              </div>
            ) : (
              <h1 className="text-4xl font-extrabold tracking-tight text-white sm:text-5xl lg:text-6xl drop-shadow-[0_2px_20px_rgba(0,0,0,0.8)]">
                {item.title}
              </h1>
            )}
          </Link>

          {metaParts.length > 0 && (
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
              {metaParts.map((part, i) => (
                <span key={part.key} className="flex items-center gap-2">
                  {i > 0 && (
                    <span aria-hidden="true" className="h-1 w-1 rounded-full bg-white/30" />
                  )}
                  {part.node}
                </span>
              ))}
            </div>
          )}

          {item.overview != null && (
            <p className="max-w-xl text-sm leading-relaxed text-white/60 line-clamp-3">
              {item.overview}
            </p>
          )}

          <div className="flex items-center gap-3 pt-1">
            <button
              type="button"
              onClick={() => navigate(playHref)}
              className="flex items-center gap-2 rounded-lg bg-white px-7 py-3 text-sm font-bold text-black transition-all hover:bg-white/90 hover:scale-105 active:scale-95"
            >
              <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
                <path d="M8 5v14l11-7z" />
              </svg>
              {t("common.play")}
            </button>
            {/* Title row doubles as the "more info" affordance — one
                primary CTA in the hero stays cleaner, and the user
                can land on the detail page by clicking the logo /
                title above. */}
            <Link
              to={detailHref}
              className="text-sm font-medium text-white/60 transition-colors hover:text-white/90"
              aria-label={t("home.viewDetails", { defaultValue: "Ver detalles" })}
            >
              {t("home.viewDetails", { defaultValue: "Ver detalles" })}
              <span aria-hidden="true" className="ml-1.5">›</span>
            </Link>
          </div>

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

// ─── Layout-driven section dispatch ───────────────────────────────────────

function renderSection(s: HomeSection) {
  if (!s.visible) return null;
  switch (s.type) {
    case "continue_watching":
      return <ContinueWatchingRail />;
    case "next_up":
      return <NextUpRail />;
    case "trending":
      return <TrendingRail />;
    case "live_now":
      return <LiveNowRail />;
    case "latest_in_library":
      if (!s.library_id) return null;
      return (
        <LatestInLibraryRail
          libraryId={s.library_id}
          libraryName={s.library_name ?? ""}
        />
      );
    default:
      return null;
  }
}

// ─── Home Page ────────────────────────────────────────────────────────────

export default function Home() {
  const { t } = useTranslation();
  // Hero candidates — kept on this page (not a rail) because the
  // hero is the page's "front door": we always want to surface
  // SOMETHING above the fold even on a fresh install with no
  // continue-watching state. Using continue + latest gives both a
  // returning user (their last show) and a cold-start user (a
  // freshly added title) a sensible default.
  const continueWatching = useContinueWatching();
  const latestItems = useLatestItems();

  const heroItems = [
    ...(continueWatching.data ?? []),
    ...(latestItems.data ?? []),
  ];

  const layout = useHomeLayout();

  const heroLoading =
    continueWatching.isLoading && latestItems.isLoading;
  const fatalError =
    layout.isError &&
    continueWatching.isError &&
    latestItems.isError &&
    heroItems.length === 0;

  if (fatalError) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 py-32 text-center">
        <svg
          className="h-12 w-12 text-white/20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
        >
          <path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
        </svg>
        <p className="text-white/50">{t("home.failedToLoad")}</p>
        <button
          type="button"
          onClick={() => {
            continueWatching.refetch();
            latestItems.refetch();
            layout.refetch();
          }}
          className="rounded-lg bg-white/10 px-5 py-2 text-sm font-medium text-white hover:bg-white/20 transition-colors"
        >
          {t("common.retry")}
        </button>
      </div>
    );
  }

  const sections = layout.data?.sections ?? [];

  return (
    <div className="flex flex-col gap-10 bg-bg-base min-h-screen -mx-4 -mb-4 md:-mx-6 md:-mb-6">
      <div className="mx-4 md:mx-6">
        {heroLoading ? (
          <div
            className="relative -mx-4 md:-mx-6 h-[70vh] min-h-[450px] max-h-[750px] bg-bg-base animate-pulse"
            style={{ marginTop: "calc(var(--topbar-height) * -1)" }}
          />
        ) : (
          <HeroBanner items={heroItems} />
        )}
      </div>

      <div className="flex flex-col gap-10 px-8 pb-12 md:px-12">
        {sections.map((s) => {
          const node = renderSection(s);
          if (!node) return null;
          return <div key={s.id}>{node}</div>;
        })}
        {/* Federated rails. Live outside the layout-driven dispatch
            for v1 because `peer_recent` / `peer_continue_watching`
            aren't registered HomeSection types yet — both self-hide
            when there's nothing to show, so a solo deployment
            renders home identically to pre-federation. Promoting
            them to configurable sections is a follow-up that needs
            the backend `validSectionType` whitelist + the
            home-layout settings UI to grow toggles for them. */}
        <PeerContinueWatchingRail />
        <PeerRecentRail />
      </div>
    </div>
  );
}
