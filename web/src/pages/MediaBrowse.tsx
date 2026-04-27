import { useState, useMemo, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useInfiniteItems } from "@/api/hooks";
import { Input, Spinner } from "@/components/common";
import { MediaGrid } from "@/components/media";
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

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        {t(`${ns}.title`)}
      </h1>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <div className="flex-1">
          <Input
            placeholder={t(`${ns}.searchPlaceholder`)}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            icon={
              <svg
                className="h-4 w-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
              >
                <circle cx="11" cy="11" r="8" />
                <path d="M21 21l-4.35-4.35" />
              </svg>
            }
          />
        </div>
        <select
          value={sort}
          onChange={(e) => setSort(e.target.value as SortOption)}
          className="rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
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
          onClick={() => setFiltersOpen((o) => !o)}
          aria-expanded={filtersOpen}
          aria-controls="media-browse-filters"
          className={[
            "rounded-[--radius-md] border px-3 py-2 text-sm transition-colors cursor-pointer",
            filterCount > 0
              ? "border-accent bg-accent/10 text-accent"
              : "border-border bg-bg-card text-text-primary hover:bg-bg-elevated",
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
