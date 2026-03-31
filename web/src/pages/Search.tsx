import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useSearch } from "@/api/hooks";
import { Input, EmptyState } from "@/components/common";
import { MediaGrid } from "@/components/media";
import { useDebounce } from "@/hooks/useDebounce";

export default function Search() {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const debouncedQuery = useDebounce(query.trim(), 300);

  const { data, isLoading } = useSearch(debouncedQuery);
  const items = data?.items ?? [];

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        {t('search.title')}
      </h1>

      <Input
        placeholder={t('search.placeholder')}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        autoFocus
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

      {!debouncedQuery ? (
        <EmptyState
          title={t('search.emptyTitle')}
          description={t('search.emptyDescription')}
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
              <circle cx="11" cy="11" r="8" />
              <path strokeLinecap="round" d="M21 21l-4.35-4.35" />
            </svg>
          }
        />
      ) : (
        <MediaGrid
          items={items}
          loading={isLoading}
          emptyMessage={t('search.noResults', { query: debouncedQuery })}
        />
      )}
    </div>
  );
}
