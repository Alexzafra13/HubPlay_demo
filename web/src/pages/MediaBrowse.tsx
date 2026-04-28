import { useState, useMemo, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
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
  const [search, setSearch] = useState("");
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

  // Browse controls — a compact search + sort + filters trio that the
  // global TopBar hoists in via TopBarSlot. Same component is rendered
  // inline as a fallback when no slot provider is present (unit tests,
  // any future shell without a global TopBar).
  const controls = (
    <BrowseControls
      ns={ns}
      search={search}
      onSearchChange={setSearch}
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
  ns: "movies" | "series";
  search: string;
  onSearchChange: (value: string) => void;
  sort: SortOption;
  onSortChange: (value: SortOption) => void;
  filterCount: number;
  filtersOpen: boolean;
  onToggleFilters: () => void;
}

// Compact horizontal control strip designed to fit in the global
// TopBar's right-aligned slot. Same primitives as the previous
// in-page bar (input + select + filters button) but sized down so
// they don't push the avatar off-screen.
function BrowseControls({
  ns,
  search,
  onSearchChange,
  sort,
  onSortChange,
  filterCount,
  filtersOpen,
  onToggleFilters,
}: BrowseControlsProps) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-2">
      <div className="relative">
        <svg
          width="14"
          height="14"
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-secondary pointer-events-none"
          aria-hidden="true"
        >
          <circle cx="8.5" cy="8.5" r="5" />
          <path d="M12.5 12.5L17 17" />
        </svg>
        <input
          type="search"
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder={t(`${ns}.searchPlaceholder`)}
          className="hidden sm:block w-44 md:w-56 lg:w-64 pl-8 pr-3 py-1.5 rounded-lg bg-bg-base border border-border text-sm text-text-primary placeholder:text-text-secondary focus:outline-none focus:ring-1 focus:ring-accent focus:border-accent transition-colors"
          aria-label={t(`${ns}.searchPlaceholder`)}
        />
      </div>
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
