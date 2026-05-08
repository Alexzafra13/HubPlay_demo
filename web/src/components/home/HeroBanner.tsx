// HeroBanner — the curated front door of the Home page.
//
// Slides aren't a flat carousel of "latest items"; they're a tiered
// shortlist where each tier expresses a clear intent so the rotation
// always exposes something the user can act on.
//
//   1. Resume   - the most recent in-progress show / movie. For episodes
//                 we render a 2-col layout: the season's poster on the
//                 left + season backdrop behind, mirroring the artwork
//                 the user sees when entering the season detail page.
//                 Deep-link goes straight to the episode (auto-play).
//   2. New      - a fresh-off-the-scanner movie or series the user has
//                 not started yet. Episodes are filtered out: a single
//                 brand-new episode buried in season 4 is not Hero
//                 material. Series duplicated by the Resume tier are
//                 dropped to avoid back-to-back slides of the same
//                 show with two different copies.
//   3. Trending - cross-peer "shared by ..." card, optional. Hidden
//                 entirely on solo deployments so the carousel never
//                 inflates with placeholder content.
//
// Rotation pauses while the pointer is over the hero so the user can
// read the overview without slides advancing under them. The slide
// indicator shows the intent tag (Reanudar / Nuevo / Trending) above
// the active dot rather than blind bullets, so the user can tell at a
// glance what each slide is going to give them.

import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Link, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";

const HERO_INTERVAL = 8000;
const MAX_SLOTS = 5;

type HeroTag = "resume" | "new" | "trending";

interface HeroSlot {
  key: string;
  tag: HeroTag;
  item: MediaItem;
}

interface HeroBannerProps {
  continueWatching: MediaItem[];
  latest: MediaItem[];
  trending?: MediaItem[];
}

// ─── Slot selection ─────────────────────────────────────────────────────────

function seriesKey(item: MediaItem): string {
  return item.series_id ?? item.id;
}

function hasUsableBackdrop(item: MediaItem): boolean {
  // Episodes lean on season/series backdrops because the episode's own
  // backdrop_url is usually the still (a small landscape thumb that
  // looks awful at hero size). Movies / series have their proper
  // backdrop_url. The selector matches what `pickBackdrop` uses below
  // so we never let through a slide that would render with a broken
  // or low-quality background.
  if (item.type === "episode") {
    return Boolean(
      item.season_backdrop_url || item.series_backdrop_url || item.backdrop_url,
    );
  }
  return Boolean(item.backdrop_url);
}

function buildSlots({
  continueWatching,
  latest,
  trending,
}: HeroBannerProps): HeroSlot[] {
  const slots: HeroSlot[] = [];
  const usedSeries = new Set<string>();

  for (const item of continueWatching) {
    if (slots.length >= MAX_SLOTS) break;
    if (!hasUsableBackdrop(item)) continue;
    const k = seriesKey(item);
    if (usedSeries.has(k)) continue;
    usedSeries.add(k);
    slots.push({ key: `resume-${item.id}`, tag: "resume", item });
  }

  for (const item of latest) {
    if (slots.length >= MAX_SLOTS) break;
    if (item.type === "episode") continue; // episodes aren't "new"-tier
    if (!hasUsableBackdrop(item)) continue;
    const k = seriesKey(item);
    if (usedSeries.has(k)) continue;
    usedSeries.add(k);
    slots.push({ key: `new-${item.id}`, tag: "new", item });
  }

  if (trending) {
    for (const item of trending) {
      if (slots.length >= MAX_SLOTS) break;
      if (!hasUsableBackdrop(item)) continue;
      const k = seriesKey(item);
      if (usedSeries.has(k)) continue;
      usedSeries.add(k);
      slots.push({ key: `trending-${item.id}`, tag: "trending", item });
    }
  }

  return slots;
}

// ─── Per-item helpers ───────────────────────────────────────────────────────

function pickBackdrop(item: MediaItem): string | null {
  if (item.type === "episode") {
    return (
      item.season_backdrop_url ?? item.series_backdrop_url ?? item.backdrop_url ?? null
    );
  }
  return item.backdrop_url ?? null;
}

function pickPoster(item: MediaItem): string | null {
  // Used by the 2-col episode layout. Prefer the season's primary
  // (the artwork the user sees when entering the season page) and
  // fall back to the series poster when the season has none.
  if (item.type === "episode") {
    return item.season_poster_url ?? item.series_poster_url ?? item.poster_url ?? null;
  }
  return item.poster_url ?? null;
}

function pickLogo(item: MediaItem): string | null {
  if (item.type === "episode") {
    // For episodes, the SERIES logo is the recognisable one; the
    // episode itself rarely has a logo. Movies use their own.
    return item.series_logo_url ?? item.logo_url ?? null;
  }
  return item.logo_url ?? null;
}

function detailHrefFor(item: MediaItem): string {
  if (item.type === "series") return `/series/${item.id}`;
  if (item.type === "episode") {
    // Detail link from a hero episode slide should land on the SERIES
    // page (not the bare episode) so the user sees the seasons grid.
    // Falls back to the episode's own item route if for some reason
    // we don't know the series id.
    return item.series_id ? `/series/${item.series_id}` : `/items/${item.id}`;
  }
  return `/movies/${item.id}`;
}

function playHrefFor(item: MediaItem): string {
  // Episodes deep-link directly to the episode's item route with
  // ?play=1; the detail page picks that up and launches the overlay.
  // Movies / series go through their own detail surface (series
  // resolves via useResumeTarget to the right next episode).
  if (item.type === "episode") return `/items/${item.id}?play=1`;
  return `${detailHrefFor(item)}?play=1`;
}

function tagLabel(tag: HeroTag, t: (k: string) => string): string {
  switch (tag) {
    case "resume":
      return t("home.heroTagResume");
    case "new":
      return t("home.heroTagNew");
    case "trending":
      return t("home.heroTagTrending");
  }
}

// Episode badge "T1 · E3" / "S1 · E3", null for non-episodes or rows
// without coordinates. Returned plain text so callers can pick the
// surrounding chip styling.
function episodeBadge(
  item: MediaItem,
  t: (k: string, opts?: Record<string, unknown>) => string,
): string | null {
  if (item.type !== "episode") return null;
  if (item.season_number == null || item.episode_number == null) return null;
  return t("home.heroEpisodeBadge", {
    season: item.season_number,
    episode: item.episode_number,
  });
}

// ─── Rendering ──────────────────────────────────────────────────────────────

interface MetaPart {
  key: string;
  node: ReactNode;
}

function buildMetaParts(item: MediaItem, hideYear: boolean): MetaPart[] {
  const parts: MetaPart[] = [];
  if (!hideYear && item.year != null && !item.title.includes(String(item.year))) {
    parts.push({
      key: "year",
      node: <span className="font-medium text-white/90">{item.year}</span>,
    });
  }
  if (item.community_rating != null) {
    parts.push({
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
    parts.push({
      key: "content",
      node: (
        <span className="rounded border border-white/30 px-1.5 py-0.5 text-[11px] font-medium text-white/80">
          {item.content_rating}
        </span>
      ),
    });
  }
  item.genres?.slice(0, 3).forEach((genre) =>
    parts.push({
      key: `genre-${genre}`,
      node: <span className="text-white/60">{genre}</span>,
    }),
  );
  return parts;
}

function MetaRow({ parts }: { parts: MetaPart[] }) {
  if (parts.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
      {parts.map((part, i) => (
        <span key={part.key} className="flex items-center gap-2">
          {i > 0 && (
            <span aria-hidden="true" className="h-1 w-1 rounded-full bg-white/30" />
          )}
          {part.node}
        </span>
      ))}
    </div>
  );
}

function ResumeProgress({ item }: { item: MediaItem }) {
  const { t } = useTranslation();
  const pct = item.user_data?.progress?.percentage;
  if (pct == null || pct <= 0) return null;
  const remainingMin =
    item.duration_ticks != null && item.duration_ticks > 0
      ? Math.max(
          0,
          Math.round(
            ((item.duration_ticks - (item.user_data?.progress?.position_ticks ?? 0)) /
              10_000_000) /
              60,
          ),
        )
      : null;
  return (
    <div className="flex flex-col gap-1.5 max-w-md">
      <div className="h-1 w-full overflow-hidden rounded-full bg-white/15">
        <div
          className="h-full bg-white transition-all"
          style={{ width: `${Math.min(100, Math.max(0, pct))}%` }}
        />
      </div>
      {remainingMin != null && remainingMin > 0 && (
        <span className="text-xs font-medium text-white/60">
          {t("home.heroRemaining", { minutes: remainingMin })}
        </span>
      )}
    </div>
  );
}

function PlayCta({
  href,
  navigate,
  resume,
  t,
}: {
  href: string;
  navigate: (to: string) => void;
  resume: boolean;
  t: (k: string) => string;
}) {
  const label = resume ? t("home.heroResumeCta") : t("common.play");
  return (
    <button
      type="button"
      onClick={() => navigate(href)}
      className="flex items-center gap-2 rounded-lg bg-white px-7 py-3 text-sm font-bold text-black transition-all hover:bg-white/90 hover:scale-105 active:scale-95"
    >
      <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
        <path d="M8 5v14l11-7z" />
      </svg>
      {label}
    </button>
  );
}

function TitleBlock({
  item,
  detailHref,
  smallTitle,
}: {
  item: MediaItem;
  detailHref: string;
  smallTitle?: string;
}) {
  const logo = pickLogo(item);
  const headline = item.type === "episode" ? item.series_title ?? item.title : item.title;
  return (
    <Link to={detailHref} className="block w-fit">
      {logo ? (
        <div className="flex flex-col gap-1">
          <img
            src={logo}
            alt={headline}
            className="max-h-16 sm:max-h-20 lg:max-h-28 max-w-[70%] w-auto object-contain object-left drop-shadow-[0_2px_20px_rgba(0,0,0,0.8)]"
          />
          {smallTitle && (
            <span className="text-base sm:text-lg font-semibold text-white/85 mt-1 drop-shadow-[0_1px_8px_rgba(0,0,0,0.8)]">
              {smallTitle}
            </span>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <h1 className="text-4xl font-extrabold tracking-tight text-white sm:text-5xl lg:text-6xl drop-shadow-[0_2px_20px_rgba(0,0,0,0.8)]">
            {headline}
          </h1>
          {smallTitle && (
            <span className="text-base sm:text-lg font-semibold text-white/85 mt-1">
              {smallTitle}
            </span>
          )}
        </div>
      )}
    </Link>
  );
}

function SlideContents({
  slot,
  navigate,
}: {
  slot: HeroSlot;
  navigate: (to: string) => void;
}) {
  const { t } = useTranslation();
  const { item, tag } = slot;
  const detailHref = detailHrefFor(item);
  const playHref = playHrefFor(item);
  const badge = episodeBadge(item, t);

  // Tag chip + episode coordinates live on the same row above the
  // headline so the user reads context (Reanudar · T1 · E3) before the
  // title — same hierarchy Plex uses on its hero spotlight.
  const tagChip = (
    <div className="flex flex-wrap items-center gap-2 text-xs font-semibold uppercase tracking-wider">
      <span className="rounded-full bg-white/15 px-3 py-1 text-white/90 backdrop-blur-sm">
        {tagLabel(tag, t)}
      </span>
      {badge && (
        <span className="rounded-full border border-white/25 px-3 py-1 text-white/80">
          {badge}
        </span>
      )}
    </div>
  );

  const isEpisode = item.type === "episode";
  const episodeSubtitle = isEpisode ? item.title : undefined;

  // Hide year next to the title when the episode subtitle is taking
  // that line; the year of the series is rarely useful for an episode
  // slide and adds visual noise.
  const metaParts = buildMetaParts(item, isEpisode);

  if (isEpisode) {
    const poster = pickPoster(item);
    return (
      <div className="absolute bottom-0 left-0 right-0 px-8 pb-12 md:px-12 md:pb-16">
        <div className="flex flex-col gap-6 md:flex-row md:items-end md:gap-8">
          {poster && (
            <div className="hidden md:block flex-shrink-0">
              <Link to={detailHref}>
                <img
                  src={poster}
                  alt=""
                  className="h-64 w-44 rounded-lg object-cover shadow-[0_10px_40px_rgba(0,0,0,0.6)] ring-1 ring-white/10 transition-transform hover:scale-[1.02]"
                />
              </Link>
            </div>
          )}
          <div className="flex max-w-2xl flex-col gap-5">
            {tagChip}
            <TitleBlock item={item} detailHref={detailHref} smallTitle={episodeSubtitle} />
            <MetaRow parts={metaParts} />
            <ResumeProgress item={item} />
            {item.overview != null && (
              <p className="max-w-xl text-sm leading-relaxed text-white/60 line-clamp-2">
                {item.overview}
              </p>
            )}
            <div className="flex items-center gap-3 pt-1">
              <PlayCta href={playHref} navigate={navigate} resume t={t} />
              <Link
                to={detailHref}
                className="text-sm font-medium text-white/60 transition-colors hover:text-white/90"
              >
                {t("home.viewDetails")}
                <span aria-hidden="true" className="ml-1.5">›</span>
              </Link>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Movie / series single-column layout. Looks like the original hero
  // but driven by the same primitives so styling stays consistent.
  return (
    <div className="absolute bottom-0 left-0 right-0 px-8 pb-12 md:px-12 md:pb-16">
      <div className="flex max-w-2xl flex-col gap-5">
        {tagChip}
        <TitleBlock item={item} detailHref={detailHref} />
        <MetaRow parts={metaParts} />
        <ResumeProgress item={item} />
        {item.overview != null && (
          <p className="max-w-xl text-sm leading-relaxed text-white/60 line-clamp-3">
            {item.overview}
          </p>
        )}
        <div className="flex items-center gap-3 pt-1">
          <PlayCta href={playHref} navigate={navigate} resume={tag === "resume"} t={t} />
          <Link
            to={detailHref}
            className="text-sm font-medium text-white/60 transition-colors hover:text-white/90"
          >
            {t("home.viewDetails")}
            <span aria-hidden="true" className="ml-1.5">›</span>
          </Link>
        </div>
      </div>
    </div>
  );
}

export function HeroBanner(props: HeroBannerProps) {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const slots = useMemo(() => buildSlots(props), [props]);

  const [activeIndex, setActiveIndex] = useState(0);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (slots.length <= 1 || paused) return;
    const timer = setInterval(() => {
      setActiveIndex((prev) => (prev + 1) % slots.length);
    }, HERO_INTERVAL);
    return () => clearInterval(timer);
  }, [slots.length, paused]);

  const goTo = useCallback((idx: number) => setActiveIndex(idx), []);

  if (slots.length === 0) return null;
  // Clamp the active index in render rather than via a reset effect
  // so we never carry a stale index after the slot list shrinks
  // (avoids cascading setState-in-effect renders for an edge-case UI).
  const safeIndex = activeIndex < slots.length ? activeIndex : 0;
  const slot = slots[safeIndex];

  return (
    <section
      className="relative -mx-4 md:-mx-6 h-[70vh] min-h-[450px] max-h-[750px] overflow-hidden"
      style={{ marginTop: "calc(var(--topbar-height) * -1)" }}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
    >
      {slots.map((s, i) => {
        const bg = pickBackdrop(s.item);
        if (!bg) return null;
        return (
          <img
            key={s.key}
            src={bg}
            alt=""
            className={`absolute inset-0 h-full w-full object-cover transition-opacity duration-1000 ${
              i === safeIndex ? "opacity-100" : "opacity-0"
            }`}
          />
        );
      })}

      <div className="absolute inset-0 bg-gradient-to-t from-bg-base from-5% via-bg-base/60 via-30% to-transparent to-70%" />
      <div className="absolute inset-0 bg-gradient-to-r from-bg-base/80 via-bg-base/20 via-50% to-transparent" />

      <SlideContents slot={slot} navigate={navigate} />

      {slots.length > 1 && (
        <div className="absolute bottom-4 left-8 right-8 md:left-12 md:right-12 flex items-center justify-center gap-2">
          {slots.map((s, i) => (
            <button
              key={s.key}
              type="button"
              onClick={() => goTo(i)}
              aria-label={tagLabel(s.tag, t)}
              className={`h-1 rounded-full transition-all duration-300 ${
                i === safeIndex
                  ? "w-10 bg-white"
                  : "w-4 bg-white/30 hover:bg-white/50"
              }`}
            />
          ))}
        </div>
      )}
    </section>
  );
}
