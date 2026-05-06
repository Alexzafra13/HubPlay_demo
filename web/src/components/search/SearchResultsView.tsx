import { useMemo } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { Film, Tv, ListVideo, Search as SearchIcon, Star, Play, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { thumb } from "@/utils/imageUrl";
import type { FederationSearchHit, MediaItem } from "@/api/types";

// Shared "grouped search results" rendering used by both the topbar
// SearchBar drawer and the dedicated /search page. Density is tuned
// for breathing room — wide cards, generous gaps, single typography
// rhythm (15px title / 12.5px metadata).

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
  /**
   * Hits from federated peers. Rendered as an extra section below
   * the local groups with a "Shared by …" badge per card. The
   * dropdown variant typically leaves this undefined and stays
   * local-only; the dedicated /search page passes both.
   */
  peerHits?: FederationSearchHit[];
  /**
   * Click handler for a peer hit. Mirrors `onItemClick` for the
   * federated section so the dropdown can dismiss itself when the
   * user picks a peer result. Kept separate from `onItemClick` so
   * call sites that only care about local hits don't have to widen
   * their type signature.
   */
  onPeerHitClick?: (hit: FederationSearchHit) => void;
}

export function SearchResultsView({
  items,
  perSectionLimit,
  onItemClick,
  peerHits,
  onPeerHitClick,
}: SearchResultsViewProps) {
  const { t } = useTranslation();
  const groups = useMemo(() => groupByType(items), [items]);

  const peerSectionItems =
    peerHits && peerHits.length > 0 && perSectionLimit != null
      ? peerHits.slice(0, perSectionLimit)
      : peerHits ?? [];

  return (
    <div className="flex flex-col gap-9">
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
      {peerSectionItems.length > 0 && (
        <PeerSection
          label={t("search.peerSection", { defaultValue: "From your peers" })}
          hits={peerSectionItems}
          totalCount={peerHits?.length ?? peerSectionItems.length}
          limited={
            perSectionLimit != null &&
            (peerHits?.length ?? 0) > perSectionLimit
          }
          onHitClick={onPeerHitClick}
        />
      )}
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
      <header className="flex items-center justify-between gap-3 mb-4">
        <div className="flex items-center gap-2.5">
          <span className="flex items-center justify-center w-7 h-7 rounded-lg bg-accent/10 ring-1 ring-accent/20">
            <Icon className="h-[15px] w-[15px] text-accent" strokeWidth={1.8} />
          </span>
          <h2 className="text-[13px] font-semibold uppercase tracking-[0.12em] text-text-primary">
            {label}
          </h2>
          <span className="text-[12px] text-text-muted">· {totalCount}</span>
        </div>
        {limited && (
          <span className="text-[11px] text-text-muted">
            mostrando {items.length}
          </span>
        )}
      </header>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
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
  const poster = thumb(item.poster_url ?? item.series_poster_url, 300);
  const href = hrefForItem(item);
  const subtitle = subtitleForItem(item);
  const meta = metaForItem(item);

  return (
    <Link
      to={href}
      onClick={() => onClick?.(item)}
      className="group relative flex items-stretch gap-5 p-3 rounded-2xl border border-border-subtle bg-bg-card/40 hover:bg-bg-card hover:border-border-strong transition-all duration-200 hover:shadow-lg hover:shadow-black/30"
    >
      {/* Poster — larger 2:3 thumb so the result row reads as a card,
          not a list line. Width matches the suggestion mini-posters
          for visual consistency through the dropdown. */}
      <div
        className="relative flex-shrink-0 w-[100px] h-[150px] rounded-lg overflow-hidden bg-bg-elevated ring-1 ring-border-subtle/60"
        style={item.poster_color ? { background: item.poster_color } : undefined}
      >
        {poster && (
          <img
            src={poster}
            alt=""
            loading="lazy"
            className="absolute inset-0 w-full h-full object-cover transition-transform duration-300 group-hover:scale-[1.04]"
          />
        )}
        {/* Subtle play affordance on hover */}
        <span className="absolute inset-0 flex items-center justify-center bg-black/45 opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <span className="flex items-center justify-center w-10 h-10 rounded-full bg-bg-base/80 backdrop-blur-sm ring-1 ring-white/10">
            <Play className="h-4 w-4 text-text-primary fill-current ml-0.5" strokeWidth={0} />
          </span>
        </span>
      </div>

      {/* Text column */}
      <div className="min-w-0 flex-1 flex flex-col justify-center py-0.5">
        <p className="text-[15px] font-semibold text-text-primary truncate group-hover:text-accent-light transition-colors leading-snug">
          {item.title}
        </p>
        {subtitle && (
          <p className="mt-1 text-[12.5px] text-text-secondary truncate">
            {subtitle}
          </p>
        )}
        {meta.length > 0 && (
          <div className="mt-2 flex items-center gap-2 text-[11.5px] text-text-muted">
            {meta.map((m, i) => (
              <span key={i} className="flex items-center gap-1">
                {i > 0 && <span className="opacity-40">·</span>}
                {m}
              </span>
            ))}
          </div>
        )}
      </div>
    </Link>
  );
}

function PeerSection({
  label,
  hits,
  totalCount,
  limited,
  onHitClick,
}: {
  label: string;
  hits: FederationSearchHit[];
  totalCount: number;
  limited: boolean;
  onHitClick?: (hit: FederationSearchHit) => void;
}) {
  return (
    <section>
      <header className="flex items-center justify-between gap-3 mb-4">
        <div className="flex items-center gap-2.5">
          <span className="flex items-center justify-center w-7 h-7 rounded-lg bg-emerald-500/10 ring-1 ring-emerald-500/30">
            <Users className="h-[15px] w-[15px] text-emerald-400" strokeWidth={1.8} />
          </span>
          <h2 className="text-[13px] font-semibold uppercase tracking-[0.12em] text-text-primary">
            {label}
          </h2>
          <span className="text-[12px] text-text-muted">· {totalCount}</span>
        </div>
        {limited && (
          <span className="text-[11px] text-text-muted">
            mostrando {hits.length}
          </span>
        )}
      </header>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {hits.map((hit) => (
          <PeerResultCard
            key={`${hit.peer_id}:${hit.id}`}
            hit={hit}
            onClick={onHitClick}
          />
        ))}
      </div>
    </section>
  );
}

function PeerResultCard({
  hit,
  onClick,
}: {
  hit: FederationSearchHit;
  onClick?: (hit: FederationSearchHit) => void;
}) {
  const { t } = useTranslation();
  // poster_url is already same-origin (proxied through /api/v1/me/peers
  // /{peer_id}/items/{id}/poster), so we can hand it to thumb() the
  // same way local items do.
  const poster = hit.poster_url ? thumb(hit.poster_url, 200) : null;
  // Detail route registered in App.tsx; libraryId is required to drive
  // the page's per-library context (back link, item lookup).
  const href = hit.library_id
    ? `/peers/${hit.peer_id}/libraries/${hit.library_id}/items/${hit.id}`
    : `/peers/${hit.peer_id}`;
  const subtitle = t("search.fromPeer", {
    defaultValue: "From {{name}}",
    name: hit.peer_name,
  });

  return (
    <Link
      to={href}
      onClick={() => onClick?.(hit)}
      className="group relative flex items-stretch gap-5 p-3 rounded-2xl border border-border-subtle bg-bg-card/40 hover:bg-bg-card hover:border-border-strong transition-all duration-200 hover:shadow-lg hover:shadow-black/30"
    >
      <div className="relative flex-shrink-0 w-[100px] h-[150px] rounded-lg overflow-hidden bg-bg-elevated ring-1 ring-border-subtle/60">
        {poster ? (
          <img
            src={poster}
            alt=""
            loading="lazy"
            className="absolute inset-0 w-full h-full object-cover transition-transform duration-300 group-hover:scale-[1.04]"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-3xl font-bold text-text-muted">
              {hit.title.charAt(0).toUpperCase()}
            </span>
          </div>
        )}
        <span className="absolute inset-0 flex items-center justify-center bg-black/45 opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <span className="flex items-center justify-center w-10 h-10 rounded-full bg-bg-base/80 backdrop-blur-sm ring-1 ring-white/10">
            <Play className="h-4 w-4 text-text-primary fill-current ml-0.5" strokeWidth={0} />
          </span>
        </span>
        <span className="absolute left-1 bottom-1 inline-flex items-center gap-1 rounded-full bg-black/65 px-1.5 py-0.5 text-[9px] font-medium text-white shadow-sm backdrop-blur-sm">
          <span className="h-1 w-1 rounded-full bg-emerald-400" aria-hidden />
          <span className="max-w-[80px] truncate">{hit.peer_name}</span>
        </span>
      </div>

      <div className="min-w-0 flex-1 flex flex-col justify-center py-0.5">
        <p className="text-[15px] font-semibold text-text-primary truncate group-hover:text-accent-light transition-colors leading-snug">
          {hit.title}
        </p>
        <p className="mt-1 text-[12.5px] text-text-secondary truncate">
          {subtitle}
        </p>
        <div className="mt-2 flex items-center gap-2 text-[11.5px] text-text-muted">
          {hit.year != null && hit.year > 0 && <span>{hit.year}</span>}
          {hit.type && (
            <>
              {hit.year != null && hit.year > 0 && <span className="opacity-40">·</span>}
              <span className="uppercase tracking-wide">{hit.type}</span>
            </>
          )}
        </div>
      </div>
    </Link>
  );
}

export function SearchNoResults({ query }: { query: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center py-16 px-6 text-center">
      <div className="flex items-center justify-center w-14 h-14 rounded-full bg-bg-elevated ring-1 ring-border-subtle mb-4">
        <SearchIcon className="h-6 w-6 text-text-muted" strokeWidth={1.5} />
      </div>
      <p className="text-[14px] text-text-secondary">
        {t("topbar.noResultsFor", { defaultValue: "Sin resultados para" })}{" "}
        <span className="text-text-primary font-semibold">"{query}"</span>
      </p>
      <p className="mt-1 text-[12px] text-text-muted">
        Prueba con otra palabra clave.
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
  // For movie/series: short single-line subtitle = first genre, if any.
  // The numeric metadata (year + rating) lives on its own row below.
  if (item.genres && item.genres.length > 0) {
    return item.genres.slice(0, 2).join(" · ");
  }
  return null;
}

function metaForItem(item: MediaItem): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  if (item.year) out.push(<>{item.year}</>);
  if (item.community_rating != null) {
    out.push(
      <>
        <Star className="h-[11px] w-[11px] text-warning fill-current" strokeWidth={0} />
        {item.community_rating.toFixed(1)}
      </>,
    );
  }
  return out;
}
