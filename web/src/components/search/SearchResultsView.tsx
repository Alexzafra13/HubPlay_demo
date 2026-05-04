import { useMemo } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { Film, Tv, ListVideo, Search as SearchIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { thumb } from "@/utils/imageUrl";
import type { MediaItem } from "@/api/types";

// Shared "grouped search results" rendering used by both the topbar
// SearchBar dropdown and the dedicated /search page. Keeps the visual
// language consistent: same section headers, same card shape, same
// subtitle conventions.

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

interface SearchResultsViewProps {
  items: MediaItem[];
  /**
   * Cap per-section results. `undefined` = show all. Use a small
   * cap (e.g. 6) when this view lives inside a dropdown.
   */
  perSectionLimit?: number;
  /** Click handler — typically used by the dropdown to dismiss itself. */
  onItemClick?: (item: MediaItem) => void;
}

export function SearchResultsView({
  items,
  perSectionLimit,
  onItemClick,
}: SearchResultsViewProps) {
  const { t } = useTranslation();
  const groups = useMemo(() => groupByType(items), [items]);

  return (
    <div className="flex flex-col gap-7">
      {SECTIONS.map((section) => {
        const all = groups[section.type];
        if (!all?.length) return null;
        const sectionItems =
          perSectionLimit != null ? all.slice(0, perSectionLimit) : all;
        return (
          <ResultSection
            key={section.type}
            label={t(section.labelKey)}
            icon={section.icon}
            items={sectionItems}
            totalCount={all.length}
            limited={perSectionLimit != null && all.length > perSectionLimit}
            onItemClick={onItemClick}
          />
        );
      })}
    </div>
  );
}

function ResultSection({
  label,
  icon: Icon,
  items,
  totalCount,
  limited,
  onItemClick,
}: {
  label: string;
  icon: LucideIcon;
  items: MediaItem[];
  totalCount: number;
  limited: boolean;
  onItemClick?: (item: MediaItem) => void;
}) {
  return (
    <section>
      <div className="flex items-center gap-2 mb-3">
        <Icon className="h-[14px] w-[14px] text-text-muted" strokeWidth={1.8} />
        <h2 className="text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted">
          {label}
        </h2>
        {limited && (
          <span className="text-[10px] text-text-muted/70">
            · {items.length} de {totalCount}
          </span>
        )}
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
        {items.map((item) => (
          <ResultCard key={item.id} item={item} onClick={onItemClick} />
        ))}
      </div>
    </section>
  );
}

function ResultCard({
  item,
  onClick,
}: {
  item: MediaItem;
  onClick?: (item: MediaItem) => void;
}) {
  const poster = thumb(item.poster_url ?? item.series_poster_url, 120);
  const href = hrefForItem(item);
  const subtitle = subtitleForItem(item);

  return (
    <Link
      to={href}
      onClick={() => onClick?.(item)}
      className="group flex items-center gap-3 p-2.5 rounded-xl border border-border-subtle bg-bg-card/40 hover:bg-bg-card hover:border-border transition-colors"
    >
      <div
        className="relative flex-shrink-0 w-[48px] h-[68px] rounded-md overflow-hidden bg-bg-elevated"
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
        <p className="text-[13.5px] font-semibold text-text-primary truncate group-hover:text-accent-light transition-colors">
          {item.title}
        </p>
        {subtitle && (
          <p className="mt-0.5 text-[11.5px] text-text-secondary truncate">
            {subtitle}
          </p>
        )}
      </div>
    </Link>
  );
}

export function SearchNoResults({ query }: { query: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center py-12 px-6 text-center">
      <SearchIcon className="h-8 w-8 text-text-muted opacity-50 mb-3" strokeWidth={1.4} />
      <p className="text-[13.5px] text-text-secondary">
        {t("topbar.noResultsFor", { defaultValue: "Sin resultados para" })}{" "}
        <span className="text-text-primary font-semibold">"{query}"</span>
      </p>
    </div>
  );
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function groupByType(items: MediaItem[]): Record<string, MediaItem[]> {
  const g: Record<string, MediaItem[]> = {};
  for (const item of items) {
    (g[item.type] ??= []).push(item);
  }
  return g;
}

function hrefForItem(item: MediaItem): string {
  if (item.type === "movie") return `/movies/${item.id}`;
  if (item.type === "series") return `/series/${item.id}`;
  return `/items/${item.id}`;
}

function subtitleForItem(item: MediaItem): string | null {
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
