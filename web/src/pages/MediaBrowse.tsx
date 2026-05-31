import { useMemo, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { useInfiniteItems } from "@/api/hooks";
import { Spinner } from "@/components/common";
import { MediaGrid } from "@/components/media";
import {
  MediaBrowseFilters,
  activeFilterCount,
  type BrowseFiltersState,
} from "@/components/media/MediaBrowseFilters";
import { SORT_OPTIONS, type SortOption } from "@/utils/sort";

type BrowseType = "movie" | "series";

interface MediaBrowseProps {
  type: BrowseType;
}

const I18N_NS: Record<BrowseType, "movies" | "series"> = {
  movie: "movies",
  series: "series",
};

// Maps client SortOption tokens to the wire `sort_by`/`sort_order`
// pair the backend understands. Centralised so the URL <-> request
// translation has a single source of truth.
const SORT_TO_WIRE: Record<SortOption, { sort_by: string; sort_order: "asc" | "desc" }> = {
  title: { sort_by: "sort_title", sort_order: "asc" },
  added: { sort_by: "added_at", sort_order: "desc" },
  year: { sort_by: "year", sort_order: "desc" },
  rating: { sort_by: "year", sort_order: "desc" }, // backend doesn't sort by rating today; fall back to year so URL is still meaningful
};

function parseSort(raw: string | null): SortOption {
  if (raw === "title" || raw === "added" || raw === "year" || raw === "rating") return raw;
  return "title";
}

// URL <-> filters bridge. Filters live in the query string so the
// page is shareable and back-button-navigable; the legacy approach
// kept them in `useState` and lost on navigation, which is exactly
// the behaviour Plex/Jellyfin avoid.
function readFiltersFromURL(params: URLSearchParams): BrowseFiltersState {
  const yearFrom = params.get("year_from");
  const yearTo = params.get("year_to");
  const minRating = params.get("min_rating");
  return {
    genre: params.get("genre") ?? "",
    yearFrom: yearFrom != null && yearFrom !== "" ? Number(yearFrom) : null,
    yearTo: yearTo != null && yearTo !== "" ? Number(yearTo) : null,
    minRating: minRating != null && minRating !== "" ? Number(minRating) : 0,
  };
}

function writeFiltersToURL(
  current: URLSearchParams,
  filters: BrowseFiltersState,
): URLSearchParams {
  const next = new URLSearchParams(current);
  // Preserve `q` and other unrelated params; only touch the filter keys.
  const setOrDelete = (key: string, value: string | null | undefined) => {
    if (value == null || value === "") next.delete(key);
    else next.set(key, value);
  };
  setOrDelete("genre", filters.genre);
  setOrDelete("year_from", filters.yearFrom != null ? String(filters.yearFrom) : "");
  setOrDelete("year_to", filters.yearTo != null ? String(filters.yearTo) : "");
  setOrDelete("min_rating", filters.minRating > 0 ? String(filters.minRating) : "");
  return next;
}

export default function MediaBrowse({ type }: MediaBrowseProps) {
  const { t } = useTranslation();
  const ns = I18N_NS[type];

  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get("q") ?? "";
  const sort = parseSort(searchParams.get("sort"));
  const filters = useMemo(() => readFiltersFromURL(searchParams), [searchParams]);
  const filtersOpen = searchParams.get("filters_open") === "1";
  // Optional library scope from the URL — the topbar dropdown links
  // here as `/movies?library_id=<id>` / `/series?library_id=<id>`
  // when the user picks a specific library to browse.
  const libraryId = searchParams.get("library_id") ?? undefined;

  // All filtering is server-side now: the page used to filter on the
  // client which silently broke once /items was paginated to 40
  // results — a 200-movie library could only ever filter the first
  // page. Filters and search go in the request params; the grid
  // shows what came back, no second pass.
  const { sort_by, sort_order } = SORT_TO_WIRE[sort];
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteItems({
      type,
      library_id: libraryId,
      sort_by,
      sort_order,
      q: search.trim() || undefined,
      genre: filters.genre || undefined,
      year_from: filters.yearFrom ?? undefined,
      year_to: filters.yearTo ?? undefined,
      min_rating: filters.minRating > 0 ? filters.minRating : undefined,
    });

  const items = useMemo(
    () => data?.pages.flatMap((page) => page.items) ?? [],
    [data],
  );

  const filterCount = activeFilterCount(filters);

  const observerRef = useRef<IntersectionObserver | null>(null);
  const sentinelRef = useCallback(
    (node: HTMLDivElement | null) => {
      if (observerRef.current) observerRef.current.disconnect();
      if (!node || !hasNextPage || isFetchingNextPage) return;

      observerRef.current = new IntersectionObserver(
        (entries) => {
          if (entries[0].isIntersecting && hasNextPage) {
            fetchNextPage();
          }
        },
        { rootMargin: "400px" },
      );
      observerRef.current.observe(node);
    },
    [hasNextPage, isFetchingNextPage, fetchNextPage],
  );

  useEffect(() => {
    return () => observerRef.current?.disconnect();
  }, []);

  const setSort = (next: SortOption) => {
    const updated = new URLSearchParams(searchParams);
    if (next === "title") updated.delete("sort"); // default — keep URL clean
    else updated.set("sort", next);
    setSearchParams(updated, { replace: true });
  };

  const setFilters = (next: BrowseFiltersState) => {
    setSearchParams(writeFiltersToURL(searchParams, next), { replace: true });
  };

  const toggleFilters = () => {
    const updated = new URLSearchParams(searchParams);
    if (filtersOpen) updated.delete("filters_open");
    else updated.set("filters_open", "1");
    setSearchParams(updated, { replace: true });
  };

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold text-text-primary sm:text-3xl">
          {t(`${ns}.title`)}
        </h1>
        <BrowseControls
          sort={sort}
          onSortChange={setSort}
          filterCount={filterCount}
          filtersOpen={filtersOpen}
          onToggleFilters={toggleFilters}
        />
      </div>

      {filtersOpen && (
        <div id="media-browse-filters">
          <MediaBrowseFilters itemType={type} state={filters} onChange={setFilters} />
        </div>
      )}

      <MediaGrid
        items={items}
        loading={isLoading}
        emptyMessage={t(`${ns}.noResults`)}
      />

      <div ref={sentinelRef} className="h-1" />
      {isFetchingNextPage && (
        <div className="flex justify-center py-4">
          <Spinner size="md" />
        </div>
      )}
    </div>
  );
}

interface BrowseControlsProps {
  sort: SortOption;
  onSortChange: (value: SortOption) => void;
  filterCount: number;
  filtersOpen: boolean;
  onToggleFilters: () => void;
}

// Compact horizontal control strip designed to fit in the global
// TopBar's right-aligned slot. Search left this trio when the global
// SearchBar took over the typing surface — sort + filter remain.
function BrowseControls({
  sort,
  onSortChange,
  filterCount,
  filtersOpen,
  onToggleFilters,
}: BrowseControlsProps) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-2">
      <select
        value={sort}
        onChange={(e) => onSortChange(e.target.value as SortOption)}
        className="rounded-lg bg-bg-base border border-border px-2 py-2 sm:py-1.5 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent focus:border-accent"
        aria-label={t("sort.by")}
      >
        {SORT_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {t(opt.labelKey)}
          </option>
        ))}
      </select>
      <button
        type="button"
        onClick={onToggleFilters}
        aria-expanded={filtersOpen}
        aria-controls="media-browse-filters"
        className={[
          "rounded-lg border px-2.5 py-2 sm:py-1.5 text-sm transition-colors cursor-pointer",
          filterCount > 0
            ? "border-accent bg-accent/10 text-accent"
            : "border-border bg-bg-base text-text-primary hover:bg-bg-elevated",
        ].join(" ")}
      >
        {t("filters.title")}
        {filterCount > 0 && (
          <span className="ml-1.5 inline-flex h-4 min-w-[1rem] items-center justify-center rounded-full bg-accent px-1 text-[10px] font-bold text-white">
            {filterCount}
          </span>
        )}
      </button>
    </div>
  );
}
