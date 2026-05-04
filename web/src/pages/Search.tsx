import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useSearch } from "@/api/hooks";
import { usePeersSearch } from "@/api/hooks/federation";
import { Spinner, EmptyState } from "@/components/common";
import { useDebounce } from "@/hooks/useDebounce";
import { SearchResultsView, SearchNoResults } from "@/components/search/SearchResultsView";

// /search results page — driven by URL ?q= so the topbar SearchBar
// (which is the only typing surface on this page) can drive the
// results without prop-drilling. Layout/cards live in
// SearchResultsView so the dropdown and this page stay in sync.
//
// Federated search runs alongside the local query. The local hook
// hits /items/search; usePeersSearch fans out to every paired peer.
// Both run in parallel — peer results trickle in (~2s ceiling per
// peer) and join the view as a "From your peers" section without
// blocking the local rail. Empty / errored peer responses are
// silently absent: a single misbehaving peer cannot blank the page.

export default function Search() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const query = searchParams.get("q") ?? "";
  const debouncedQuery = useDebounce(query.trim(), 220);

  const { data, isFetching } = useSearch(debouncedQuery);
  const items = data ?? [];
  const peers = usePeersSearch(debouncedQuery);
  const peerHits = peers.data?.hits ?? [];

  // Total count shown next to the heading reflects everything the
  // page is rendering — local + peer — so the user gets one honest
  // number instead of two competing counts.
  const totalCount = items.length + peerHits.length;
  const hasAny = items.length > 0 || peerHits.length > 0;
  // Initial spinner only when nothing is on screen yet; once we have
  // something to render, late-arriving peer hits join inline without
  // dropping the local results back into a spinner.
  const showInitialSpinner =
    (isFetching || peers.isFetching) && !hasAny;

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
        {debouncedQuery && hasAny && !showInitialSpinner && (
          <span className="text-[13px] text-text-muted">
            {t("search.totalCount", {
              defaultValue: "{{count}} elementos",
              count: totalCount,
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
      ) : showInitialSpinner ? (
        <div className="flex items-center justify-center py-24">
          <Spinner size="md" />
        </div>
      ) : !hasAny ? (
        <SearchNoResults query={debouncedQuery} />
      ) : (
        <SearchResultsView items={items} peerHits={peerHits} />
      )}
    </div>
  );
}
