import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { m, AnimatePresence } from "framer-motion";
import {
  Search as SearchIcon,
  X,
  ArrowRight,
  Loader2,
} from "lucide-react";
import {
  useSearch,
  useContinueWatching,
  useHomeTrending,
  useHomeRecommended,
} from "@/api/hooks";
import { usePeersSearch } from "@/api/hooks/federation";
import type { MediaItem, HomeTrendingItem, HomeRecommendedItem } from "@/api/types";
import { useDebounce } from "@/hooks/useDebounce";
import {
  SearchResultsView,
  SearchNoResults,
} from "@/components/search/SearchResultsView";

// SearchBar — collapsed by default to a single magnifier icon. Click
// expands the icon into an inline input (spring animation). Typing
// drops a full-width panel from below the topbar with grouped
// results — same layout as the dedicated /search page so the user
// gets a consistent experience between the dropdown and the page.
//
// Behavior modes:
//   · /movies, /series, /search, /live-tv  → "filter mode": value
//     mirrored to URL `?q=`, no dropdown (the page is the result
//     surface). On /live-tv the query filters channels + programmes
//     in place.
//   · everywhere else             → "search mode": local query,
//     full-width dropdown, Enter goes to /search?q=…

const FILTER_ROUTES = ["/movies", "/series", "/search", "/live-tv"];

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

  const [localQuery, setLocalQuery] = useState("");
  const query = filterMode ? urlQuery : localQuery;

  const inputRef = useRef<HTMLInputElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const debounced = useDebounce(query.trim(), 220);
  const dropdownActive = open && !filterMode && debounced.length > 0;
  const suggestionsActive = open && !filterMode && query.length === 0;

  const { data, isFetching } = useSearch(debounced, {
    enabled: dropdownActive,
    staleTime: 30_000,
  });
  const results = useMemo(() => data ?? [], [data]);
  // Federated hits run alongside the local query while the dropdown
  // is open. The fetch is conditional on dropdownActive so we don't
  // fan out to every paired peer for an empty / closed bar. Late
  // peer hits join the panel under "From your peers" without
  // disturbing the local rail above them.
  const peers = usePeersSearch(debounced, dropdownActive);
  const peerHits = peers.data?.hits ?? [];
  const dropdownLoading = isFetching || peers.isFetching;
  const dropdownEmpty =
    !dropdownLoading && results.length === 0 && peerHits.length === 0;

  // Auto-expand if the URL already has ?q= when the page mounts
  // (filter route deep link, or user reloaded the tab mid-search).
  // Render-time guarded setState — derives `open` from URL state
  // instead of running an effect on every render.
  const urlKey = (filterMode ? "f:" : "") + urlQuery;
  const [lastUrlKey, setLastUrlKey] = useState(urlKey);
  if (lastUrlKey !== urlKey) {
    setLastUrlKey(urlKey);
    if (filterMode && urlQuery.length > 0 && !open) {
      setOpen(true);
    }
  }

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
  }, []);

  // Outside-click closes the dropdown AND collapses the bar if empty.
  // The dropdown lives outside `wrapRef` (it's a full-width fixed
  // panel below the topbar), so we have to whitelist its container too
  // — handled by giving the dropdown its own ref and checking both.
  const dropdownRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    function onDoc(e: MouseEvent) {
      const target = e.target as Node;
      if (
        wrapRef.current?.contains(target) ||
        dropdownRef.current?.contains(target)
      ) {
        return;
      }
      // Closed-without-collapse if the bar is non-empty so the user
      // doesn't lose their query when dismissing the dropdown.
      if (query.length === 0) {
        setOpen(false);
      } else if (dropdownActive || suggestionsActive) {
        // Just close the panel; keep the bar wide and the value.
        // (We toggle `open` off then back on synchronously to drop
        // the panel without losing the input width — but actually
        // the panel hides naturally because dropdownActive depends
        // on `open`. To dismiss without unmounting the bar, leave
        // open=true but rely on a separate "panelClosed" flag.)
        // Simplest: keep open=true, but the panel re-opens on focus.
        // For now, just close everything; the user can click again.
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open, query, dropdownActive, suggestionsActive]);

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

  function handleSubmit(e?: React.FormEvent) {
    if (e) e.preventDefault();
    const trimmed = query.trim();
    if (!trimmed) return;
    if (!filterMode) {
      navigate(`/search?q=${encodeURIComponent(trimmed)}`);
      setLocalQuery("");
      setOpen(false);
    }
  }

  // Page-aware placeholder.
  const placeholder = useMemo(() => {
    if (filterMode) {
      if (location.pathname.startsWith("/search")) {
        return t("topbar.searchPlaceholder");
      }
      if (location.pathname.startsWith("/live-tv")) {
        return t("liveTV.searchPlaceholder", {
          defaultValue: "Busca canales o programas…",
        });
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

  const panelOpen = dropdownActive || suggestionsActive;

  return (
    <>
      {/* Reserved 36×36 slot in the topbar flex flow — the expanded
          search panel is absolute right-0 inside it, so growing left
          to 320px never displaces the centered MainNav (the previous
          layout pushed siblings every time the bar opened).
          Estilo expandido: pill (rounded-full) con ring sutil en
          reposo y glow del color accent cuando el input tiene foco,
          para que enseñar el campo no sea una caja gris cualquiera. */}
      <div ref={wrapRef} className="relative size-9 flex-shrink-0">
        <m.div
          layout
          initial={false}
          animate={{ width: open ? 320 : 36 }}
          transition={{ type: "spring", stiffness: 380, damping: 32, mass: 0.6 }}
          className={[
            "group absolute right-0 top-0 h-9 flex items-center rounded-full overflow-hidden transition-[background-color,box-shadow] duration-200",
            open
              ? "bg-bg-overlay/95 backdrop-blur-xl ring-1 ring-white/10 shadow-[0_6px_20px_-6px_rgba(0,0,0,0.55)]"
              : "bg-bg-hover/40 ring-1 ring-white/8 hover:ring-white/20",
          ].join(" ")}
        >
          <button
            type="button"
            onClick={() => (open ? inputRef.current?.focus() : openSearch())}
            className="flex items-center justify-center size-9 flex-shrink-0 text-text-secondary hover:text-text-primary transition-colors"
            aria-label={t("nav.search")}
            aria-expanded={open}
          >
            <SearchIcon className="size-[17px]" strokeWidth={1.8} />
          </button>

          {open && (
            <form onSubmit={handleSubmit} className="flex-1 flex items-center pr-1.5">
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
                  }
                }}
                placeholder={placeholder}
                className="flex-1 min-w-0 bg-transparent border-none outline-none text-[13px] text-text-primary placeholder:text-text-muted/80 p-0"
                autoComplete="off"
                spellCheck={false}
              />
              {dropdownLoading && dropdownActive && (
                <Loader2 className="size-3.5 mx-1.5 text-text-muted animate-spin flex-shrink-0" strokeWidth={1.8} />
              )}
              {query.length > 0 ? (
                <button
                  type="button"
                  onClick={() => setQuery("")}
                  className="flex items-center justify-center size-6 rounded-full text-text-muted hover:text-text-primary hover:bg-white/8 transition-colors flex-shrink-0"
                  aria-label={t("common.cancel")}
                >
                  <X className="size-3.5" strokeWidth={2} />
                </button>
              ) : (
                <button
                  type="button"
                  onClick={closeAndClear}
                  className="flex items-center justify-center flex-shrink-0"
                  aria-label={t("nav.closeMenu")}
                >
                  <kbd className="px-1.5 py-0.5 rounded-md text-[10px] font-medium text-text-muted bg-white/5 ring-1 ring-white/10 hover:text-text-primary hover:ring-white/25 transition-colors">
                    Esc
                  </kbd>
                </button>
              )}
            </form>
          )}
        </m.div>
      </div>

      {/* Full-width drop-down panel anchored to the bottom of the
          topbar. Lives outside the bar's relative parent so it can
          stretch the entire viewport width — Plex-style drawer. */}
      <AnimatePresence>
        {panelOpen && (
          <>
            {/* Backdrop — subtle dim of the page underneath, just
                enough to focus the eye on the panel without making
                the rest of the UI look offline. Starts below the
                topbar so the bar stays clickable. */}
            <m.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.14 }}
              className="fixed inset-0 z-40 bg-black/35 backdrop-blur-[3px]"
              style={{ top: "var(--topbar-height)" }}
              aria-hidden
            />

            <m.div
              ref={dropdownRef}
              initial={{ opacity: 0, y: -8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -8 }}
              transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
              className="fixed left-0 right-0 z-50 bg-bg-overlay/95 backdrop-blur-2xl border-b border-border shadow-2xl shadow-black/60"
              style={{ top: "var(--topbar-height)" }}
              role="region"
              aria-label={t("nav.search")}
            >
              <div className="max-w-[1400px] mx-auto p-6 max-h-[calc(100dvh-var(--topbar-height)-24px)] overflow-y-auto">
                {dropdownActive ? (
                  dropdownEmpty ? (
                    <SearchNoResults query={debounced} />
                  ) : (
                    <>
                      <SearchResultsView
                        items={results}
                        peerHits={peerHits}
                        perSectionLimit={6}
                        onItemClick={() => {
                          setLocalQuery("");
                          setOpen(false);
                        }}
                        onPeerHitClick={() => {
                          setLocalQuery("");
                          setOpen(false);
                        }}
                      />
                      <button
                        type="button"
                        onClick={() => handleSubmit()}
                        className="mt-6 w-full flex items-center justify-center gap-2 h-11 rounded-lg border border-border-subtle text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                      >
                        <span>
                          {t("topbar.viewAllResults", {
                            defaultValue: "Ver todos los resultados",
                          })}
                        </span>
                        <kbd className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-bg-base/60 border border-border-subtle">
                          Enter
                        </kbd>
                        <ArrowRight className="size-3.5" strokeWidth={1.7} />
                      </button>
                    </>
                  )
                ) : (
                  <SuggestionsPanel
                    onPick={(href) => {
                      navigate(href);
                      setOpen(false);
                    }}
                  />
                )}
              </div>
            </m.div>
          </>
        )}
      </AnimatePresence>
    </>
  );
}

// ─── Empty-state suggestions ────────────────────────────────────────────────
//
// Pre-typing state. Three blocks layered top-to-bottom: "Continúa
// viendo" mini-posters, "Tendencias" mini-posters, and the section
// shortcut tiles (Películas/Series/TV). Each rail self-hides when its
// hook has nothing to show, so a brand-new user falls back to just
// the section tiles — same behaviour as before but enriched.

function SuggestionsPanel({ onPick }: { onPick: (href: string) => void }) {
  const { data: continueWatching } = useContinueWatching({ staleTime: 60_000 });
  const { data: trending } = useHomeTrending({ staleTime: 60_000 });
  // Recomendados sustituye a las tarjetas "Películas / Series / TV en
  // vivo" que vivían al final del panel — los tabs de la topbar ya
  // ofrecen esos atajos y duplicarlos aquí no aportaba nada útil.
  // Recomendados sí es contenido genuino: una rail de títulos
  // sugeridos para el usuario.
  const { data: recommended } = useHomeRecommended({ staleTime: 60_000 });

  const continueRow = (continueWatching ?? []).slice(0, 6);
  const trendingRow = (trending ?? []).slice(0, 8);
  const recommendedRow = (recommended ?? []).slice(0, 8);

  return (
    <div className="flex flex-col gap-7">
      {continueRow.length > 0 && (
        <SuggestionRail
          labelKey="topbar.suggestionContinue"
          fallbackLabel="Continua viendo"
          items={continueRow.map(mediaToPick)}
          onPick={onPick}
        />
      )}

      {trendingRow.length > 0 && (
        <SuggestionRail
          labelKey="topbar.suggestionTrending"
          fallbackLabel="Tendencias"
          items={trendingRow.map(trendingToPick)}
          onPick={onPick}
        />
      )}

      {recommendedRow.length > 0 && (
        <SuggestionRail
          labelKey="topbar.suggestionRecommended"
          fallbackLabel="Recomendados"
          items={recommendedRow.map(recommendedToPick)}
          onPick={onPick}
        />
      )}
    </div>
  );
}

interface PickItem {
  id: string;
  title: string;
  href: string;
  posterUrl?: string;
  posterColor?: string;
  year?: number;
}

function mediaToPick(it: MediaItem): PickItem {
  const href = it.type === "series" ? `/series/${it.id}` : `/movies/${it.id}`;
  return {
    id: it.id,
    title: it.title,
    href,
    posterUrl: it.poster_url ?? it.series_poster_url ?? undefined,
    posterColor: it.poster_color ?? undefined,
    year: it.year ?? undefined,
  };
}

function trendingToPick(it: HomeTrendingItem): PickItem {
  const href = it.type === "series" ? `/series/${it.id}` : `/movies/${it.id}`;
  return {
    id: it.id,
    title: it.title,
    href,
    posterUrl: it.poster_url,
    posterColor: it.poster_color,
    year: it.year,
  };
}

function recommendedToPick(it: HomeRecommendedItem): PickItem {
  const href = it.type === "series" ? `/series/${it.id}` : `/movies/${it.id}`;
  return {
    id: it.id,
    title: it.title,
    href,
    posterUrl: it.poster_url,
    posterColor: it.poster_color,
    year: it.year,
  };
}

function SuggestionRail({
  labelKey,
  fallbackLabel,
  items,
  onPick,
}: {
  labelKey: string;
  fallbackLabel: string;
  items: PickItem[];
  onPick: (href: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <div>
      <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted mb-3">
        {t(labelKey, { defaultValue: fallbackLabel })}
      </p>
      <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 gap-3">
        {items.map((it) => (
          <button
            key={it.id}
            type="button"
            onClick={() => onPick(it.href)}
            className="group flex flex-col gap-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent rounded-md"
          >
            <div
              className="relative aspect-[2/3] overflow-hidden rounded-md ring-1 ring-border-subtle/60 bg-bg-elevated transition-transform duration-200 group-hover:scale-[1.04]"
              style={it.posterColor ? { background: it.posterColor } : undefined}
            >
              {it.posterUrl && (
                <img
                  src={it.posterUrl}
                  alt={it.title}
                  loading="lazy"
                  decoding="async"
                  className="absolute inset-0 size-full object-cover"
                />
              )}
            </div>
            <p className="text-[11.5px] font-medium text-text-secondary group-hover:text-text-primary truncate transition-colors">
              {it.title}
            </p>
          </button>
        ))}
      </div>
    </div>
  );
}
