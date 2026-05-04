import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useSearch } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";
import { useDebounce } from "@/hooks/useDebounce";
import { SearchResultsView, SearchNoResults } from "@/components/search/SearchResultsView";

// /search results page — driven by URL ?q= so the topbar SearchBar
// (which is the only typing surface on this page) can drive the
// results without prop-drilling. Layout/cards live in
// SearchResultsView so the dropdown and this page stay in sync.

export default function Search() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const query = searchParams.get("q") ?? "";
  const debouncedQuery = useDebounce(query.trim(), 220);

  const { data, isFetching } = useSearch(debouncedQuery);
  const items = data ?? [];

  return (
    <div className="flex flex-col gap-8 px-6 py-8 sm:px-10 max-w-[1400px] mx-auto w-full">
      <header className="flex items-baseline gap-3 flex-wrap">
        <h1
          className="text-[26px] sm:text-[28px] font-semibold tracking-tight text-text-primary"
          style={{ letterSpacing: "-0.015em" }}
        >
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
        <SearchNoResults query={debouncedQuery} />
      ) : (
        <SearchResultsView items={items} />
      )}
    </div>
  );
}
