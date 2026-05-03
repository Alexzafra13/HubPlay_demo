import { useState, useMemo, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { useInfiniteItems } from "@/api/hooks";
import { Spinner } from "@/components/common";
import { MediaGrid } from "@/components/media";
import { useTopBarSlot } from "@/components/layout/TopBarSlot";
import {
  MediaBrowseFilters,
  applyFilters,
  activeFilterCount,
  emptyFilters,
  type BrowseFiltersState,
} from "@/components/media/MediaBrowseFilters";
import { sortItems, SORT_OPTIONS, type SortOption } from "@/utils/sort";

export type BrowseType = "movie" | "series";

interface MediaBrowseProps {
  type: BrowseType;
}

const I18N_NS: Record<BrowseType, "movies" | "series"> = {
  movie: "movies",
  series: "series",
};

export default function MediaBrowse({ type }: MediaBrowseProps) {
  const { t } = useTranslation();
  const ns = I18N_NS[type];

  // Search lives in the URL (`?q=`) so the topbar SearchBar — which is
  // the only place the user types on these pages — can drive the
  // grid filter without prop-drilling or a shared store. The URL also
  // makes the filter shareable: hand a teammate /movies?q=batman.
  const [searchParams] = useSearchParams();
  const search = searchParams.get("q") ?? "";

  const [sort, setSort] = useState<SortOption>("added");
  const [filters, setFilters] = useState<BrowseFiltersState>(emptyFilters);
  const [filtersOpen, setFiltersOpen] = useState(false);

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteItems({ type });

  const items = useMemo(
    () => data?.pages.flatMap((page) => page.items) ?? [],
    [data],
  );

  const filtered = useMemo(() => {
    let result = items;
    if (search.trim()) {
      const q = search.toLowerCase();
      result = result.filter(
        (item) =>
          item.title.toLowerCase().includes(q) ||
          (item.original_title?.toLowerCase().includes(q) ?? false),
      );
    }
    result = applyFilters(result, filters);
    return sortItems(result, sort);
  }, [items, search, sort, filters]);

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

  // Sort + filters for the topbar slot. The search input is gone:
  // the global SearchBar in the topbar owns it now and writes the
  // value to URL `?q=` which we read above.
  const controls = (
    <BrowseControls
      sort={sort}
      onSortChange={setSort}
      filterCount={filterCount}
      filtersOpen={filtersOpen}
      onToggleFilters={() => setFiltersOpen((o) => !o)}
    />
  );
  const slotActive = useTopBarSlot(controls);

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
          {t(`${ns}.title`)}
        </h1>
      </div>

      {/* Inline-controls fallback for environments without a TopBar
          provider — keeps the page usable in standalone tests / future
          shells while the slot path stays the production default. */}
      {!slotActive && <div>{controls}</div>}

      {filtersOpen && (
        <div id="media-browse-filters">
          <MediaBrowseFilters items={items} state={filters} onChange={setFilters} />
        </div>
      )}

      <MediaGrid
        items={filtered}
        loading={isLoading}
        emptyMessage={t(`${ns}.noResults`)}
      />

      {!search.trim() && (
        <>
          <div ref={sentinelRef} className="h-1" />
          {isFetchingNextPage && (
            <div className="flex justify-center py-4">
              <Spinner size="md" />
            </div>
          )}
        </>
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
        className="rounded-lg bg-bg-base border border-border px-2 py-1.5 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent focus:border-accent"
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
          "rounded-lg border px-2.5 py-1.5 text-sm transition-colors cursor-pointer",
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
