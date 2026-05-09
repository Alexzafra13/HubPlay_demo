// HMR caveat: this file exports both the component and helper
// types/constants reached by other modules. Splitting solely for
// Fast Refresh would over-fragment the filter surface.
/* eslint-disable react-refresh/only-export-components */
import { useState, useEffect } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { useGenres } from "@/api/hooks";

// Server-driven filter state. Each field is the wire shape passed
// straight to /items — keep these names aligned with the backend
// query params so the URL <-> hook <-> request flow has no rename
// step in the middle.
export interface BrowseFiltersState {
  /**
   * Selected genre name (case-insensitive, exact match).
   * Server-side filtering is single-genre for now — multi-select
   * needs an `IN` rewrite of the WHERE clause and isn't worth the
   * cost until users ask for it. Empty string disables.
   */
  genre: string;
  /** Year inclusive bounds; null = no constraint on that side. */
  yearFrom: number | null;
  yearTo: number | null;
  /** Minimum community rating (0..10). 0 = no constraint. */
  minRating: number;
}

export const emptyFilters: BrowseFiltersState = {
  genre: "",
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
  if (f.genre) n++;
  if (f.yearFrom != null || f.yearTo != null) n++;
  if (f.minRating > 0) n++;
  return n;
}

interface MediaBrowseFiltersProps {
  /** "movie" | "series" — scopes the genre vocabulary. */
  itemType?: string;
  state: BrowseFiltersState;
  onChange: (next: BrowseFiltersState) => void;
}

/**
 * Filter panel below the sort/search toolbar. The genre vocabulary
 * comes from `/items/genres` (server-aggregated over the whole
 * catalogue) — the previous implementation derived it from the
 * 40 already-loaded items, which silently broke on libraries with
 * more than one page of content.
 */
const MediaBrowseFilters: FC<MediaBrowseFiltersProps> = ({ itemType, state, onChange }) => {
  const { t } = useTranslation();
  const { data: genres } = useGenres(itemType);

  // Local input state for the year fields. Bound directly to the
  // input element so the user can clear or partially-type a value
  // without triggering a debounced refetch on every keystroke;
  // commits to the parent state on blur or Enter.
  const [yearFromInput, setYearFromInput] = useState<string>(
    state.yearFrom?.toString() ?? "",
  );
  const [yearToInput, setYearToInput] = useState<string>(
    state.yearTo?.toString() ?? "",
  );
  // Sync local input when external state changes (e.g. URL deep-
  // link). The lint rule (set-state-in-effect) flags this but the
  // alternative — keying the inputs by the URL state — would
  // re-mount on every keystroke and lose focus mid-typing.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    setYearFromInput(state.yearFrom?.toString() ?? "");
    setYearToInput(state.yearTo?.toString() ?? "");
  }, [state.yearFrom, state.yearTo]);
  /* eslint-enable react-hooks/set-state-in-effect */

  const cap = 20; // chip cap matches the previous client-side derivation
  const genreOptions = (genres ?? []).slice(0, cap);

  const toggleGenre = (g: string) => {
    onChange({ ...state, genre: state.genre === g ? "" : g });
  };

  const commitYearFrom = () => {
    const n = yearFromInput === "" ? null : Number(yearFromInput);
    onChange({ ...state, yearFrom: Number.isFinite(n!) ? (n as number) : null });
  };
  const commitYearTo = () => {
    const n = yearToInput === "" ? null : Number(yearToInput);
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
          {state.genre && (
            <button
              type="button"
              onClick={() => onChange({ ...state, genre: "" })}
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
              const active = state.genre === name;
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
              placeholder={t("filters.from")}
              value={yearFromInput}
              onChange={(e) => setYearFromInput(e.target.value)}
              onBlur={commitYearFrom}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitYearFrom();
              }}
              className="w-24 rounded-[--radius-md] border border-border bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
              aria-label={t("filters.from")}
            />
            <span className="text-text-muted">–</span>
            <input
              type="number"
              inputMode="numeric"
              placeholder={t("filters.to")}
              value={yearToInput}
              onChange={(e) => setYearToInput(e.target.value)}
              onBlur={commitYearTo}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitYearTo();
              }}
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
