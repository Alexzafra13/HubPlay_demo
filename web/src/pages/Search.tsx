import { useMemo } from "react";
import { Link, useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { Film, Tv, ListVideo, Search as SearchIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useSearch } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";
import { useDebounce } from "@/hooks/useDebounce";
import { thumb } from "@/utils/imageUrl";
import type { MediaItem } from "@/api/types";

// /search results page — grouped by media type, each section a 3-col
// grid of horizontal cards. Driven by URL ?q= so the topbar SearchBar
// (which is the only typing surface on this page) can drive the
// results without prop-drilling.

interface SectionDef {
  type: MediaItem["type"];
  labelKey: string;
  icon: LucideIcon;
}

const SECTIONS: SectionDef[] = [
  { type: "movie", labelKey: "nav.movies", icon: Film },
  { type: "series", labelKey: "nav.series", icon: Tv },
  { type: "episode", labelKey: "search.episodes", icon: ListVideo },
];

export default function Search() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const query = searchParams.get("q") ?? "";
  const debouncedQuery = useDebounce(query.trim(), 220);

  const { data, isFetching } = useSearch(debouncedQuery);
  const items = data ?? [];

  const groups = useMemo(() => {
    const g: Record<string, MediaItem[]> = {};
    for (const item of items) {
      const key = item.type;
      (g[key] ??= []).push(item);
    }
    return g;
  }, [items]);

  return (
    <div className="flex flex-col gap-8 px-6 py-8 sm:px-10 max-w-[1400px] mx-auto w-full">
      {/* Header — title only. The topbar SearchBar is the input. */}
      <header className="flex items-baseline gap-3 flex-wrap">
        <h1 className="text-[26px] sm:text-[28px] font-semibold tracking-tight text-text-primary"
          style={{ letterSpacing: "-0.015em" }}>
          {debouncedQuery
            ? t("search.resultsFor", {
                defaultValue: 'Resultados para "{{query}}"',
                query: debouncedQuery,
              })
            : t("search.title")}
        </h1>
        {debouncedQuery && items.length > 0 && !isFetching && (
          <span className="text-[13px] text-text-muted">
            {t("search.totalCount", {
              defaultValue: "{{count}} elementos",
              count: items.length,
            })}
          </span>
        )}
      </header>

      {!debouncedQuery ? (
        <EmptyState
          title={t("search.emptyTitle")}
          description={t("search.emptyDescription")}
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
              <circle cx="11" cy="11" r="8" />
              <path strokeLinecap="round" d="M21 21l-4.35-4.35" />
            </svg>
          }
        />
      ) : isFetching && items.length === 0 ? (
        <div className="flex items-center justify-center py-24">
          <Spinner size="md" />
        </div>
      ) : items.length === 0 ? (
        <NoResults query={debouncedQuery} />
      ) : (
        <div className="flex flex-col gap-10">
          {SECTIONS.map((section) => {
            const sectionItems = groups[section.type];
            if (!sectionItems?.length) return null;
            return (
              <ResultSection
                key={section.type}
                label={t(section.labelKey)}
                icon={section.icon}
                items={sectionItems}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}

// ─── Sections ───────────────────────────────────────────────────────────────

function ResultSection({
  label,
  icon: Icon,
  items,
}: {
  label: string;
  icon: LucideIcon;
  items: MediaItem[];
}) {
  return (
    <section>
      <div className="flex items-center gap-2 mb-4">
        <Icon className="h-[14px] w-[14px] text-text-muted" strokeWidth={1.8} />
        <h2 className="text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted">
          {label}
        </h2>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-3">
        {items.map((item) => (
          <ResultCard key={item.id} item={item} />
        ))}
      </div>
    </section>
  );
}

function ResultCard({ item }: { item: MediaItem }) {
  const poster = thumb(item.poster_url ?? item.series_poster_url, 120);
  const href =
    item.type === "movie"
      ? `/movies/${item.id}`
      : item.type === "series"
        ? `/series/${item.id}`
        : `/items/${item.id}`;

  // Subtitle: for episodes show "Series · S01E03"; for series the
  // year if available; for movies year. Fallback to capitalized type.
  const subtitle = useSubtitle(item);

  return (
    <Link
      to={href}
      className="group flex items-center gap-3 p-2.5 rounded-xl border border-border-subtle bg-bg-card/40 hover:bg-bg-card hover:border-border transition-colors"
    >
      <div
        className="relative flex-shrink-0 w-[52px] h-[72px] rounded-md overflow-hidden bg-bg-elevated"
        style={item.poster_color ? { background: item.poster_color } : undefined}
      >
        {poster && (
          <img
            src={poster}
            alt=""
            loading="lazy"
            className="absolute inset-0 w-full h-full object-cover"
          />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-[14px] font-semibold text-text-primary truncate group-hover:text-accent-light transition-colors">
          {item.title}
        </p>
        {subtitle && (
          <p className="mt-0.5 text-[12px] text-text-secondary truncate">{subtitle}</p>
        )}
      </div>
    </Link>
  );
}

function useSubtitle(item: MediaItem): string | null {
  if (item.type === "episode") {
    const parts: string[] = [];
    if (item.series_title) parts.push(item.series_title);
    const code =
      item.season_number != null && item.episode_number != null
        ? `S${String(item.season_number).padStart(2, "0")}E${String(item.episode_number).padStart(2, "0")}`
        : null;
    if (code) parts.push(code);
    return parts.join(" · ") || null;
  }
  if (item.year) return String(item.year);
  return null;
}

function NoResults({ query }: { query: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center py-24 px-6 text-center">
      <SearchIcon className="h-10 w-10 text-text-muted opacity-50 mb-4" strokeWidth={1.4} />
      <p className="text-[15px] text-text-secondary">
        {t("topbar.noResultsFor", { defaultValue: "Sin resultados para" })}{" "}
        <span className="text-text-primary font-semibold">"{query}"</span>
      </p>
    </div>
  );
}
