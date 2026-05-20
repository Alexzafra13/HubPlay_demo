import { useTranslation } from "react-i18next";
import {
  HeroSettings,
  type HeroMode,
  type HeroModeOption,
} from "./HeroSettings";

export type ViewTab = "inicio" | "explorar";

interface LiveTvTopBarProps {
  tab: ViewTab;
  onTab: (t: ViewTab) => void;
  search: string;
  onSearch: (s: string) => void;
  totalChannels: number;
  liveNow: number;
  heroMode: HeroMode;
  heroModeOptions: HeroModeOption[];
  onHeroModeChange: (mode: HeroMode) => void;
  /** "Solo favoritos" toggle — only relevant on the Explorar tab.
   * Inicio doesn't expose it because the hero already promotes
   * favourites via the spotlight strategy. */
  favoritesOnly?: boolean;
  onFavoritesOnlyChange?: (v: boolean) => void;
}

/**
 * LiveTvTopBar — page header for the Live TV surfaces.
 *
 * Renders a sticky page subbar (title + counts + tabs + search +
 * hero-mode picker) directly inside the page. Previously hoisted into
 * the global TopBar via TopBarSlot, but the topbar now owns the main
 * navigation and dropdown panels so page-level controls can't share
 * that real estate without conflicts. Living inside the page gives
 * each surface its own breathable header without a portal hop.
 *
 * Stateless wrt filters and mode — the parent page owns `tab`,
 * `search`, `heroMode`. This component only wires onChange callbacks
 * up to the input controls and draws WAI-ARIA `role=tab` / `aria-selected`.
 */
export function LiveTvTopBar({
  tab,
  onTab,
  search,
  onSearch,
  totalChannels,
  liveNow,
  heroMode,
  heroModeOptions,
  onHeroModeChange,
  favoritesOnly = false,
  onFavoritesOnlyChange,
}: LiveTvTopBarProps) {
  const { t } = useTranslation();
  // Two-surface design (Plex-style): "Inicio" lands on a hero +
  // schedule grid that answers "what's on right now?", "Explorar"
  // surfaces the full channel lineup with categories + favourites
  // filter for browsing.
  const tabs: { id: ViewTab; label: string }[] = [
    { id: "inicio", label: t("liveTV.tab.inicio", { defaultValue: "Inicio" }) },
    {
      id: "explorar",
      label: t("liveTV.tab.explorar", { defaultValue: "Explorar" }),
    },
  ];

  const controls = (
    <div className="flex flex-wrap items-center gap-2 md:gap-3">
      <label className="relative flex items-center">
        <span className="sr-only">{t("liveTV.searchPlaceholder")}</span>
        <SearchIcon />
        <input
          type="search"
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder={t("liveTV.searchPlaceholder", {
            defaultValue: "Busca canales o programas…",
          })}
          className="w-56 lg:w-72 rounded-full border border-tv-line bg-tv-bg-1 py-1.5 pl-9 pr-3 text-sm text-tv-fg-0 placeholder:text-tv-fg-3 focus:border-tv-accent focus:outline-none focus:ring-2 focus:ring-tv-accent/30"
        />
      </label>

      <div
        role="tablist"
        aria-label={t("liveTV.viewMode", { defaultValue: "Vista" })}
        className="flex items-center gap-1 rounded-full border border-tv-line bg-tv-bg-1 p-1"
      >
        {tabs.map((it) => (
          <button
            key={it.id}
            role="tab"
            aria-selected={tab === it.id}
            type="button"
            onClick={() => onTab(it.id)}
            className={[
              "rounded-full px-3 py-1 text-xs font-medium transition-colors",
              tab === it.id
                ? "bg-accent text-white shadow-sm"
                : "text-tv-fg-1 hover:text-tv-fg-0",
            ].join(" ")}
          >
            {it.label}
          </button>
        ))}
      </div>

      {/* Favourites-only filter — only on Explorar (Inicio surfaces
          favourites via the hero spotlight strategy). The toggle
          lives in the topbar so the user can flip it without scrolling. */}
      {tab === "explorar" && onFavoritesOnlyChange ? (
        <button
          type="button"
          aria-pressed={favoritesOnly}
          onClick={() => onFavoritesOnlyChange(!favoritesOnly)}
          className={[
            "inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
            favoritesOnly
              ? "border-tv-accent/60 bg-tv-accent/15 text-tv-accent"
              : "border-tv-line bg-tv-bg-1 text-tv-fg-1 hover:text-tv-fg-0",
          ].join(" ")}
        >
          <HeartIcon filled={favoritesOnly} />
          {t("liveTV.favoritesOnly", { defaultValue: "Solo favoritos" })}
        </button>
      ) : null}

      {/* Hero view settings — surfaced on Inicio because that's where
          the spotlight lives. Living here (not inside the hero itself)
          keeps it reachable even when the viewer has hidden the
          spotlight entirely — an in-hero gear would vanish with the
          hero, making "ocultar" a dead end. */}
      {tab === "inicio" ? (
        <HeroSettings
          mode={heroMode}
          modeOptions={heroModeOptions}
          onModeChange={onHeroModeChange}
        />
      ) : null}
    </div>
  );

  // On Inicio the title + counters are lifted into the hero's top-left
  // overlay so the hero hugs the topbar; on Explorar we show an inline
  // page header with channel counts.
  const headerCopy =
    tab === "explorar"
      ? {
          title: t("liveTV.titleExplorar", {
            defaultValue: "Explorar canales",
          }),
          subtitle: (
            <>
              <b className="text-tv-fg-1">{totalChannels}</b>{" "}
              {t("liveTV.channels", { defaultValue: "canales" })} ·{" "}
              <b className="text-tv-fg-1">{liveNow}</b>{" "}
              {t("liveTV.liveNow", { defaultValue: "en vivo ahora" })}
            </>
          ),
        }
      : null;

  return (
    <header className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      {headerCopy && (
        <div>
          <h1 className="flex items-center gap-2 text-xl font-bold text-tv-fg-0 md:text-2xl">
            <span className="inline-flex size-2.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
            {headerCopy.title}
          </h1>
          {headerCopy.subtitle ? (
            <p className="mt-1 text-xs text-tv-fg-2">{headerCopy.subtitle}</p>
          ) : null}
        </div>
      )}

      {controls}
    </header>
  );
}

function HeartIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" />
    </svg>
  );
}

function SearchIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="absolute left-3 top-1/2 -translate-y-1/2 text-tv-fg-3"
      aria-hidden="true"
    >
      <circle cx="8.5" cy="8.5" r="5" />
      <path d="M12.5 12.5L17 17" />
    </svg>
  );
}
