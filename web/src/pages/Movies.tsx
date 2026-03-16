import { useState, useMemo } from "react";
import { useItems } from "@/api/hooks";
import { Input } from "@/components/common";
import { MediaGrid } from "@/components/media";
import { sortItems, SORT_OPTIONS, type SortOption } from "@/utils/sort";

export default function Movies() {
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortOption>("added");

  const { data, isLoading } = useItems({ type: "movie", limit: 50 });
  const items = data?.items ?? [];

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
    return sortItems(result, sort);
  }, [items, search, sort]);

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        Movies
      </h1>

      {/* Toolbar */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <div className="flex-1">
          <Input
            placeholder="Search movies..."
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
        >
          {SORT_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>

      <MediaGrid items={filtered} loading={isLoading} emptyMessage="No movies found" />
    </div>
  );
}
