import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import { Search as SearchIcon, X, Film, Tv, Radio, ArrowRight, Loader2 } from "lucide-react";
import { useSearch } from "@/api/hooks";
import { useDebounce } from "@/hooks/useDebounce";
import { thumb } from "@/utils/imageUrl";
import type { MediaItem } from "@/api/types";

// SearchPopover — global search overlay. Triggered by the topbar lupa
// button or ⌘K from anywhere. Anchored top-right under the topbar so
// the user's eye doesn't have to travel after clicking the trigger.
//
// Behavior:
//   · Empty input  → suggestions panel (quick links + recent).
//   · Typing       → debounced /items/search call, top 8 with poster.
//   · Submit       → navigates to /search?q=… (full results page).
//   · Click result → navigates to the item's detail page.
//   · Esc / outside → closes.
//   · ↑/↓/Enter    → keyboard nav over suggestions or results.
//
// Context-aware hint at the top: "Buscando en HubPlay" by default;
// "Buscando en Películas" when the user is on /movies, etc. — the
// query still runs globally; we just nudge the user toward the
// current section so the experience feels page-relevant.

interface SearchPopoverProps {
  open: boolean;
  onClose: () => void;
}

export function SearchPopover({ open, onClose }: SearchPopoverProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();

  const [query, setQuery] = useState("");
  const [highlightedIndex, setHighlightedIndex] = useState(0);
  const debounced = useDebounce(query.trim(), 220);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const { data, isFetching } = useSearch(debounced, {
    staleTime: 30_000,
  });
  const results = useMemo(() => (data ?? []).slice(0, 8), [data]);

  // Reset state on open and steal focus to the input.
  useEffect(() => {
    if (!open) return;
    setQuery("");
    setHighlightedIndex(0);
    const t = window.setTimeout(() => inputRef.current?.focus(), 30);
    return () => window.clearTimeout(t);
  }, [open]);

  // Outside click + Esc.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    }
    function onDoc(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDoc);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDoc);
    };
  }, [open, onClose]);

  // Reset highlight when results change so ↑/↓ doesn't point at a
  // stale row index that no longer exists.
  useEffect(() => {
    setHighlightedIndex(0);
  }, [debounced, results.length]);

  // Page-aware hint copy.
  const sectionHint = useMemo(() => {
    const path = location.pathname;
    if (path.startsWith("/movies")) return t("nav.movies");
    if (path.startsWith("/series")) return t("nav.series");
    if (path.startsWith("/live-tv")) return t("nav.liveTV");
    return "HubPlay";
  }, [location.pathname, t]);

  const showResults = debounced.length > 0;
  const itemsToNavigate: MediaItem[] = showResults ? results : [];

  function handleSubmit(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (showResults && itemsToNavigate[highlightedIndex]) {
      goToItem(itemsToNavigate[highlightedIndex]);
      return;
    }
    if (query.trim()) {
      navigate(`/search?q=${encodeURIComponent(query.trim())}`);
      onClose();
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
    onClose();
  }

  function onArrow(dir: 1 | -1) {
    const max = itemsToNavigate.length - 1;
    if (max < 0) return;
    setHighlightedIndex((idx) => {
      const next = idx + dir;
      if (next < 0) return max;
      if (next > max) return 0;
      return next;
    });
  }

  return (
    <AnimatePresence>
      {open && (
        <>
          {/* Backdrop — softer than a modal; hint of focus, not blocking */}
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-50 bg-black/40 backdrop-blur-[2px]"
            style={{ top: "var(--topbar-height)" }}
            aria-hidden
          />

          <motion.div
            ref={containerRef}
            initial={{ opacity: 0, y: -8, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -8, scale: 0.98 }}
            transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
            className="fixed left-1/2 -translate-x-1/2 z-50 w-[min(640px,calc(100vw-24px))] rounded-2xl border border-border bg-bg-overlay/95 backdrop-blur-2xl shadow-2xl shadow-black/60 overflow-hidden"
            style={{ top: "calc(var(--topbar-height) + 12px)" }}
            role="dialog"
            aria-modal="false"
            aria-label={t("nav.search")}
          >
            <form onSubmit={handleSubmit}>
              <div className="flex items-center gap-3 px-4 h-14 border-b border-border-subtle">
                <SearchIcon className="h-[18px] w-[18px] text-text-muted flex-shrink-0" strokeWidth={1.7} />
                <input
                  ref={inputRef}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "ArrowDown") {
                      e.preventDefault();
                      onArrow(1);
                    } else if (e.key === "ArrowUp") {
                      e.preventDefault();
                      onArrow(-1);
                    }
                  }}
                  placeholder={
                    sectionHint === "HubPlay"
                      ? t("topbar.searchPlaceholder")
                      : `${t("topbar.searchPlaceholder")} · ${sectionHint}`
                  }
                  className="flex-1 bg-transparent border-none outline-none text-[15px] text-text-primary placeholder:text-text-muted"
                  autoComplete="off"
                  spellCheck={false}
                />
                {isFetching && showResults && (
                  <Loader2 className="h-4 w-4 text-text-muted animate-spin" strokeWidth={1.8} />
                )}
                <button
                  type="button"
                  onClick={onClose}
                  className="p-1 rounded-md text-text-muted hover:text-text-primary hover:bg-bg-hover transition-colors"
                  aria-label={t("nav.closeMenu")}
                >
                  <X className="h-4 w-4" strokeWidth={1.7} />
                </button>
              </div>
            </form>

            <div className="max-h-[min(520px,70vh)] overflow-y-auto">
              {!showResults ? (
                <SuggestionsPanel
                  onPick={(href) => {
                    onClose();
                    navigate(href);
                  }}
                />
              ) : results.length === 0 && !isFetching ? (
                <EmptyResults query={debounced} />
              ) : (
                <ResultsList
                  items={itemsToNavigate}
                  highlightedIndex={highlightedIndex}
                  onHover={setHighlightedIndex}
                  onPick={goToItem}
                />
              )}
            </div>

            {showResults && results.length > 0 && (
              <button
                type="button"
                onClick={() => handleSubmit()}
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
        </>
      )}
    </AnimatePresence>
  );
}

// ─── Suggestions (empty state) ──────────────────────────────────────────────

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
                <ArrowRight className="h-3.5 w-3.5 text-text-muted opacity-0 group-hover:opacity-100" strokeWidth={1.6} />
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
    <div className="flex flex-col items-center justify-center py-12 px-6 text-center">
      <SearchIcon className="h-7 w-7 text-text-muted opacity-50 mb-3" strokeWidth={1.4} />
      <p className="text-[13px] text-text-secondary">
        {t("topbar.noResultsFor", { defaultValue: "Sin resultados para" })}{" "}
        <span className="text-text-primary font-medium">"{query}"</span>
      </p>
    </div>
  );
}

// ─── Results list ───────────────────────────────────────────────────────────

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
  const typeLabel = item.type === "movie" ? "Película" : item.type === "series" ? "Serie" : item.type;

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
          style={
            item.poster_color
              ? { background: item.poster_color }
              : undefined
          }
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
