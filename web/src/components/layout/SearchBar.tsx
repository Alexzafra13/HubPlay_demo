import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import {
  Search as SearchIcon,
  X,
  ArrowRight,
  Loader2,
  Film,
  Tv,
  Radio,
} from "lucide-react";
import { useSearch } from "@/api/hooks";
import { useDebounce } from "@/hooks/useDebounce";
import { thumb } from "@/utils/imageUrl";
import type { MediaItem } from "@/api/types";

// SearchBar — collapsed by default to a single magnifier icon. Clicking
// it animates the icon out into a full input, focused. Typing produces
// either:
//   · a dropdown (anchored below the topbar) on Home and most pages, or
//   · a URL-driven `?q=` filter on /movies and /series (the page reads
//     it and narrows its grid in-place — no dropdown).
//
// The mode flip is intentional: on browse pages the user already has a
// grid in view, so an overlay would feel like an extra step. On Home
// or pages without a grid, an inline dropdown with poster previews is
// the fastest path to "click the thing I meant".

// Routes whose grid reads `?q=` directly. Typing in the SearchBar on
// these routes writes to the URL instead of opening a dropdown — the
// page IS the result surface. /search is included so the dedicated
// results page stays in sync with the topbar input.
const FILTER_ROUTES = ["/movies", "/series", "/search"];

function isFilterRoute(pathname: string): boolean {
  return FILTER_ROUTES.some(
    (p) => pathname === p || pathname.startsWith(p + "/"),
  );
}

export function SearchBar() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();

  const filterMode = isFilterRoute(location.pathname);
  const urlQuery = searchParams.get("q") ?? "";
  const [open, setOpen] = useState(urlQuery.length > 0);
  const [highlightedIndex, setHighlightedIndex] = useState(0);

  // Local query state mirrors the URL on filter pages (so the input
  // value always reflects what's actually filtering the grid). On
  // non-filter pages we keep an internal-only state so visiting
  // /search?q=foo doesn't get its query mirrored back into the topbar.
  const [localQuery, setLocalQuery] = useState("");
  const query = filterMode ? urlQuery : localQuery;

  const inputRef = useRef<HTMLInputElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const debounced = useDebounce(query.trim(), 220);
  const dropdownActive = open && !filterMode && debounced.length > 0;

  const { data, isFetching } = useSearch(debounced, {
    enabled: dropdownActive,
    staleTime: 30_000,
  });
  const results = useMemo(() => (data ?? []).slice(0, 8), [data]);

  // Reset highlight when results change so ↑/↓ doesn't point at a
  // stale row index that no longer exists.
  useEffect(() => {
    setHighlightedIndex(0);
  }, [debounced, results.length]);

  // Auto-expand if the URL already has ?q= when the page mounts
  // (filter route deep link, or user reloaded the tab mid-search).
  useEffect(() => {
    if (filterMode && urlQuery.length > 0 && !open) {
      setOpen(true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterMode, urlQuery]);

  // ⌘K / Ctrl+K opens from anywhere.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        openSearch();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Outside-click collapses the bar back to icon ONLY if it's empty.
  // Keeping it expanded when there's a value avoids the user's text
  // disappearing if they click on a result they then dismiss.
  useEffect(() => {
    if (!open) return;
    function onDoc(e: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        if (query.length === 0) setOpen(false);
      }
    }
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open, query]);

  function openSearch() {
    setOpen(true);
    requestAnimationFrame(() => inputRef.current?.focus());
  }

  function closeAndClear() {
    if (filterMode) {
      const next = new URLSearchParams(searchParams);
      next.delete("q");
      setSearchParams(next, { replace: true });
    } else {
      setLocalQuery("");
    }
    setOpen(false);
  }

  function setQuery(value: string) {
    if (filterMode) {
      const next = new URLSearchParams(searchParams);
      if (value) next.set("q", value);
      else next.delete("q");
      setSearchParams(next, { replace: true });
    } else {
      setLocalQuery(value);
    }
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = query.trim();
    if (!trimmed) return;
    if (dropdownActive && results[highlightedIndex]) {
      goToItem(results[highlightedIndex]);
      return;
    }
    if (!filterMode) {
      navigate(`/search?q=${encodeURIComponent(trimmed)}`);
      setLocalQuery("");
      setOpen(false);
    }
  }

  function goToItem(item: MediaItem) {
    const base =
      item.type === "movie"
        ? "/movies"
        : item.type === "series"
          ? "/series"
          : "/items";
    navigate(`${base}/${item.id}`);
    setLocalQuery("");
    setOpen(false);
  }

  function onArrow(dir: 1 | -1) {
    if (!dropdownActive) return;
    const max = results.length - 1;
    if (max < 0) return;
    setHighlightedIndex((idx) => {
      const next = idx + dir;
      if (next < 0) return max;
      if (next > max) return 0;
      return next;
    });
  }

  // Page-aware placeholder — same query field, but we tell the user
  // what the current behavior is.
  const placeholder = useMemo(() => {
    if (filterMode) {
      if (location.pathname.startsWith("/search")) {
        return t("topbar.searchPlaceholder");
      }
      const section = location.pathname.startsWith("/series")
        ? t("nav.series")
        : t("nav.movies");
      return t("topbar.filterPlaceholder", {
        defaultValue: `Filtrar en ${section}…`,
        section,
      });
    }
    return t("topbar.searchPlaceholder");
  }, [filterMode, location.pathname, t]);

  return (
    <div ref={wrapRef} className="relative">
      <motion.div
        layout
        initial={false}
        animate={{ width: open ? 280 : 36 }}
        transition={{ type: "spring", stiffness: 380, damping: 32, mass: 0.6 }}
        className={[
          "h-9 flex items-center rounded-lg overflow-hidden border transition-colors",
          open
            ? "bg-bg-overlay border-border-strong shadow-lg shadow-black/30"
            : "bg-bg-hover/40 border-border-subtle hover:border-border",
        ].join(" ")}
      >
        <button
          type="button"
          onClick={() => (open ? inputRef.current?.focus() : openSearch())}
          className="flex items-center justify-center w-9 h-9 flex-shrink-0 text-text-secondary hover:text-text-primary transition-colors"
          aria-label={t("nav.search")}
          aria-expanded={open}
        >
          <SearchIcon className="h-[17px] w-[17px]" strokeWidth={1.7} />
        </button>

        {open && (
          <form onSubmit={handleSubmit} className="flex-1 flex items-center">
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault();
                  if (query) {
                    setQuery("");
                  } else {
                    setOpen(false);
                  }
                } else if (e.key === "ArrowDown") {
                  e.preventDefault();
                  onArrow(1);
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  onArrow(-1);
                }
              }}
              placeholder={placeholder}
              className="flex-1 min-w-0 bg-transparent border-none outline-none text-[13px] text-text-primary placeholder:text-text-muted px-0 py-0"
              autoComplete="off"
              spellCheck={false}
            />
            {isFetching && dropdownActive && (
              <Loader2 className="h-3.5 w-3.5 mx-1.5 text-text-muted animate-spin flex-shrink-0" strokeWidth={1.8} />
            )}
            {query.length > 0 ? (
              <button
                type="button"
                onClick={() => setQuery("")}
                className="flex items-center justify-center w-7 h-7 mr-1 rounded-md text-text-muted hover:text-text-primary hover:bg-bg-hover transition-colors flex-shrink-0"
                aria-label={t("common.cancel")}
              >
                <X className="h-3.5 w-3.5" strokeWidth={1.8} />
              </button>
            ) : (
              <button
                type="button"
                onClick={closeAndClear}
                className="flex items-center justify-center px-1.5 mr-1 text-[10px] font-medium text-text-muted hover:text-text-primary transition-colors flex-shrink-0"
                aria-label={t("nav.closeMenu")}
              >
                Esc
              </button>
            )}
          </form>
        )}
      </motion.div>

      <AnimatePresence>
        {dropdownActive && (
          <motion.div
            initial={{ opacity: 0, y: -6, scale: 0.985 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.985 }}
            transition={{ duration: 0.16, ease: [0.32, 0.72, 0, 1] }}
            className="absolute right-0 mt-2 w-[420px] max-w-[calc(100vw-32px)] rounded-xl border border-border bg-bg-overlay/95 backdrop-blur-2xl shadow-2xl shadow-black/50 overflow-hidden z-50"
            style={{ top: "100%" }}
            role="listbox"
            aria-label={t("nav.search")}
          >
            <div className="max-h-[min(480px,65vh)] overflow-y-auto">
              {results.length === 0 && !isFetching ? (
                <EmptyResults query={debounced} />
              ) : (
                <ResultsList
                  items={results}
                  highlightedIndex={highlightedIndex}
                  onHover={setHighlightedIndex}
                  onPick={goToItem}
                />
              )}
            </div>

            {results.length > 0 && (
              <button
                type="button"
                onClick={() => handleSubmit({ preventDefault: () => {} } as React.FormEvent)}
                className="w-full flex items-center justify-between gap-2 px-4 h-11 border-t border-border-subtle text-[12.5px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
              >
                <span>
                  {t("topbar.viewAllResults", { defaultValue: "Ver todos los resultados" })}
                </span>
                <span className="flex items-center gap-1.5">
                  <kbd className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-bg-base/60 border border-border-subtle">
                    Enter
                  </kbd>
                  <ArrowRight className="h-3.5 w-3.5" strokeWidth={1.7} />
                </span>
              </button>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      {/* Empty-state suggestion drawer — only when expanded with no
          query yet, on non-filter routes. Helps the user discover
          the section quick-links. */}
      <AnimatePresence>
        {open && !filterMode && query.length === 0 && (
          <motion.div
            initial={{ opacity: 0, y: -6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.14 }}
            className="absolute right-0 mt-2 w-[280px] max-w-[calc(100vw-32px)] rounded-xl border border-border bg-bg-overlay/95 backdrop-blur-2xl shadow-2xl shadow-black/50 overflow-hidden z-50"
            style={{ top: "100%" }}
          >
            <SuggestionsPanel
              onPick={(href) => {
                navigate(href);
                setOpen(false);
              }}
            />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ─── Sub-components ─────────────────────────────────────────────────────────

function SuggestionsPanel({ onPick }: { onPick: (href: string) => void }) {
  const { t } = useTranslation();
  const items = [
    { href: "/movies", icon: Film, label: t("nav.movies") },
    { href: "/series", icon: Tv, label: t("nav.series") },
    { href: "/live-tv", icon: Radio, label: t("nav.liveTV") },
  ];
  return (
    <div className="p-2">
      <p className="px-3 pt-2 pb-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted">
        {t("topbar.suggestions", { defaultValue: "Sugerencias" })}
      </p>
      <ul className="space-y-0.5">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <li key={item.href}>
              <button
                onClick={() => onPick(item.href)}
                className="w-full flex items-center gap-3 px-3 h-10 rounded-lg text-left text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
              >
                <Icon className="h-4 w-4 text-text-muted" strokeWidth={1.6} />
                <span className="flex-1 truncate">{item.label}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function EmptyResults({ query }: { query: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center py-10 px-6 text-center">
      <SearchIcon className="h-7 w-7 text-text-muted opacity-50 mb-3" strokeWidth={1.4} />
      <p className="text-[13px] text-text-secondary">
        {t("topbar.noResultsFor", { defaultValue: "Sin resultados para" })}{" "}
        <span className="text-text-primary font-medium">"{query}"</span>
      </p>
    </div>
  );
}

function ResultsList({
  items,
  highlightedIndex,
  onHover,
  onPick,
}: {
  items: MediaItem[];
  highlightedIndex: number;
  onHover: (i: number) => void;
  onPick: (item: MediaItem) => void;
}) {
  return (
    <ul className="p-2">
      {items.map((item, i) => (
        <ResultRow
          key={item.id}
          item={item}
          isHighlighted={i === highlightedIndex}
          onMouseEnter={() => onHover(i)}
          onClick={() => onPick(item)}
        />
      ))}
    </ul>
  );
}

function ResultRow({
  item,
  isHighlighted,
  onMouseEnter,
  onClick,
}: {
  item: MediaItem;
  isHighlighted: boolean;
  onMouseEnter: () => void;
  onClick: () => void;
}) {
  const poster = thumb(item.poster_url ?? item.series_poster_url, 80);
  const typeLabel =
    item.type === "movie" ? "Película" : item.type === "series" ? "Serie" : item.type;

  return (
    <li>
      <button
        onMouseEnter={onMouseEnter}
        onClick={onClick}
        className={[
          "w-full flex items-center gap-3 p-2 rounded-lg text-left transition-colors",
          isHighlighted ? "bg-bg-active" : "hover:bg-bg-hover",
        ].join(" ")}
      >
        <div
          className="relative flex-shrink-0 w-10 h-14 rounded-md overflow-hidden bg-bg-elevated"
          style={item.poster_color ? { background: item.poster_color } : undefined}
        >
          {poster && (
            <img
              src={poster}
              alt=""
              loading="lazy"
              className="absolute inset-0 w-full h-full object-cover"
            />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-[13.5px] font-medium text-text-primary truncate">
            {item.title}
          </p>
          <div className="mt-0.5 flex items-center gap-2 text-[11.5px] text-text-muted">
            <span className="capitalize">{typeLabel}</span>
            {item.year && (
              <>
                <span className="opacity-40">·</span>
                <span>{item.year}</span>
              </>
            )}
            {item.community_rating != null && (
              <>
                <span className="opacity-40">·</span>
                <span>★ {item.community_rating.toFixed(1)}</span>
              </>
            )}
          </div>
        </div>
        {isHighlighted && (
          <ArrowRight className="h-4 w-4 text-text-muted flex-shrink-0" strokeWidth={1.6} />
        )}
      </button>
    </li>
  );
}
