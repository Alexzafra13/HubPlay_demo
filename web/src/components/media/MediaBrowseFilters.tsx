import { useMemo } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";

export interface BrowseFiltersState {
  /** Selected genres (case-insensitive). Empty set = no genre filter. */
  genres: Set<string>;
  /** Year inclusive bounds. null = no constraint on that side. */
  yearFrom: number | null;
  yearTo: number | null;
  /** Minimum community rating (0..10). 0 = no constraint. */
  minRating: number;
}

export const emptyFilters: BrowseFiltersState = {
  genres: new Set(),
  yearFrom: null,
  yearTo: null,
  minRating: 0,
};

/**
 * Returns the count of active filter categories so the toolbar can
 * render a "(N)" badge on the Filters button. Cheaper than passing
 * the whole state around just for a UI hint.
 */
export function activeFilterCount(f: BrowseFiltersState): number {
  let n = 0;
  if (f.genres.size > 0) n++;
  if (f.yearFrom != null || f.yearTo != null) n++;
  if (f.minRating > 0) n++;
  return n;
}

/**
 * Pure filter — applies the state to a list of items. Items with
 * unknown year / rating bypass the corresponding constraint (we
 * don't have grounds to exclude them just because metadata is
 * missing). Genre match is "any" (item must have at least one of
 * the selected genres), not "all" — Plex / Jellyfin both default
 * to OR for the same UX reason: AND filters quickly produce empty
 * results and frustrate the user.
 */
export function applyFilters(items: MediaItem[], f: BrowseFiltersState): MediaItem[] {
  if (activeFilterCount(f) === 0) return items;
  const wantGenres = Array.from(f.genres).map((g) => g.toLowerCase());
  return items.filter((item) => {
    if (wantGenres.length > 0) {
      const itemGenres = (item.genres ?? []).map((g) => g.toLowerCase());
      const hit = wantGenres.some((g) => itemGenres.includes(g));
      if (!hit) return false;
    }
    if (f.yearFrom != null && item.year != null && item.year < f.yearFrom) return false;
    if (f.yearTo != null && item.year != null && item.year > f.yearTo) return false;
    if (f.minRating > 0) {
      // Items without a rating fall through. Otherwise the slider
      // would silently hide unrated items, which is rarely what the
      // user wants and impossible to discover.
      if (item.community_rating != null && item.community_rating < f.minRating) {
        return false;
      }
    }
    return true;
  });
}

interface MediaBrowseFiltersProps {
  /** All items currently loaded (so we can derive the genre vocabulary). */
  items: MediaItem[];
  state: BrowseFiltersState;
  onChange: (next: BrowseFiltersState) => void;
}

/**
 * Render-time filter panel. Lives below the sort/search toolbar in
 * MediaBrowse; the parent controls visibility via a toggle button so
 * the panel stays out of the way for users who never use it.
 *
 * Keeps the genre vocabulary derived from the actual loaded items
 * rather than a hardcoded list — different libraries (TMDb-tagged
 * vs NFO-tagged) use different spellings and the user should see
 * what they have, not a guess.
 */
const MediaBrowseFilters: FC<MediaBrowseFiltersProps> = ({ items, state, onChange }) => {
  const { t } = useTranslation();

  // Derive genre vocabulary from items. Sorted by frequency desc so
  // the most useful chips appear first. Cap the visible set at 20 —
  // libraries with deep TMDb metadata can hit ~50+ unique genres,
  // most of which the user will never tap.
  const genreOptions = useMemo(() => {
    const counts = new Map<string, number>();
    for (const item of items) {
      for (const g of item.genres ?? []) {
        const key = g.trim();
        if (!key) continue;
        counts.set(key, (counts.get(key) ?? 0) + 1);
      }
    }
    return Array.from(counts.entries())
      .sort((a, b) => b[1] - a[1])
      .slice(0, 20)
      .map(([name, count]) => ({ name, count }));
  }, [items]);

  // Year bounds derived once for the input placeholders (so the user
  // sees what range their library actually covers).
  const { earliestYear, latestYear } = useMemo(() => {
    let lo = Number.POSITIVE_INFINITY;
    let hi = Number.NEGATIVE_INFINITY;
    for (const item of items) {
      if (item.year != null) {
        if (item.year < lo) lo = item.year;
        if (item.year > hi) hi = item.year;
      }
    }
    return {
      earliestYear: isFinite(lo) ? lo : null,
      latestYear: isFinite(hi) ? hi : null,
    };
  }, [items]);

  const toggleGenre = (g: string) => {
    const next = new Set(state.genres);
    if (next.has(g)) next.delete(g);
    else next.add(g);
    onChange({ ...state, genres: next });
  };

  const setYearFrom = (raw: string) => {
    const n = raw === "" ? null : Number(raw);
    onChange({ ...state, yearFrom: Number.isFinite(n!) ? (n as number) : null });
  };
  const setYearTo = (raw: string) => {
    const n = raw === "" ? null : Number(raw);
    onChange({ ...state, yearTo: Number.isFinite(n!) ? (n as number) : null });
  };
  const setMinRating = (n: number) => onChange({ ...state, minRating: n });

  const clearAll = () => onChange(emptyFilters);

  return (
    <div className="flex flex-col gap-4 rounded-[--radius-md] border border-border bg-bg-card/60 p-4">
      {/* Genre chips */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">
            {t("filters.genre")}
          </p>
          {state.genres.size > 0 && (
            <button
              type="button"
              onClick={() => onChange({ ...state, genres: new Set() })}
              className="text-xs text-text-muted hover:text-text-primary cursor-pointer"
            >
              {t("filters.clear")}
            </button>
          )}
        </div>
        {genreOptions.length === 0 ? (
          <p className="text-xs text-text-muted">{t("filters.noGenres")}</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {genreOptions.map(({ name, count }) => {
              const active = state.genres.has(name);
              return (
                <button
                  key={name}
                  type="button"
                  onClick={() => toggleGenre(name)}
                  className={[
                    "px-2.5 py-1 rounded-full text-xs font-medium transition-colors cursor-pointer",
                    active
                      ? "bg-accent text-white"
                      : "bg-bg-elevated text-text-secondary hover:text-text-primary",
                  ].join(" ")}
                  title={t("filters.genreCount", { count })}
                >
                  {name}
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Year range + Min rating */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">
            {t("filters.year")}
          </p>
          <div className="flex items-center gap-2">
            <input
              type="number"
              inputMode="numeric"
              placeholder={earliestYear?.toString() ?? t("filters.from")}
              value={state.yearFrom ?? ""}
              onChange={(e) => setYearFrom(e.target.value)}
              className="w-24 rounded-[--radius-md] border border-border bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
              aria-label={t("filters.from")}
            />
            <span className="text-text-muted">–</span>
            <input
              type="number"
              inputMode="numeric"
              placeholder={latestYear?.toString() ?? t("filters.to")}
              value={state.yearTo ?? ""}
              onChange={(e) => setYearTo(e.target.value)}
              className="w-24 rounded-[--radius-md] border border-border bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
              aria-label={t("filters.to")}
            />
          </div>
        </div>

        <div>
          <p className="mb-2 flex items-center justify-between text-xs font-semibold uppercase tracking-wide text-text-muted">
            <span>{t("filters.rating")}</span>
            <span className="font-mono normal-case tracking-normal">
              {state.minRating > 0 ? `≥ ${state.minRating.toFixed(1)}` : t("filters.any")}
            </span>
          </p>
          <input
            type="range"
            min={0}
            max={10}
            step={0.5}
            value={state.minRating}
            onChange={(e) => setMinRating(Number(e.target.value))}
            className="w-full accent-accent cursor-pointer"
            aria-label={t("filters.rating")}
          />
        </div>
      </div>

      {activeFilterCount(state) > 0 && (
        <button
          type="button"
          onClick={clearAll}
          className="self-end text-xs text-text-muted hover:text-text-primary cursor-pointer"
        >
          {t("filters.clearAll")}
        </button>
      )}
    </div>
  );
};

export { MediaBrowseFilters };
