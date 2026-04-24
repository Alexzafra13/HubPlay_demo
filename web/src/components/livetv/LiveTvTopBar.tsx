import { useTranslation } from "react-i18next";
import {
  HeroSettings,
  type HeroMode,
  type HeroModeOption,
} from "./HeroSettings";

export type ViewTab = "discover" | "guide" | "favorites";

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
}

/**
 * LiveTvTopBar — page header for the Live TV surfaces: title + live
 * counts, search input, tab switcher, and the per-account hero picker
 * (only rendered on the Discover tab so the gear isn't offering a
 * control whose effect isn't visible).
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
}: LiveTvTopBarProps) {
  const { t } = useTranslation();
  const tabs: { id: ViewTab; label: string }[] = [
    {
      id: "discover",
      label: t("liveTV.tab.discover", { defaultValue: "Descubrir" }),
    },
    { id: "guide", label: t("liveTV.tab.guide", { defaultValue: "Guía" }) },
    {
      id: "favorites",
      label: t("liveTV.tab.favorites", { defaultValue: "Favoritos" }),
    },
  ];

  return (
    <header className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h1 className="flex items-center gap-2 text-xl font-bold text-tv-fg-0 md:text-2xl">
          <span className="inline-flex h-2.5 w-2.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
          {t("liveTV.title", { defaultValue: "TV en directo" })}
        </h1>
        <p className="mt-1 text-xs text-tv-fg-2">
          <b className="text-tv-fg-1">{totalChannels}</b>{" "}
          {t("liveTV.channels", { defaultValue: "canales" })} ·{" "}
          <b className="text-tv-fg-1">{liveNow}</b>{" "}
          {t("liveTV.liveNow", { defaultValue: "en vivo ahora" })}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
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
            className="w-72 rounded-full border border-tv-line bg-tv-bg-1 py-2 pl-9 pr-3 text-sm text-tv-fg-0 placeholder:text-tv-fg-3 focus:border-tv-accent focus:outline-none focus:ring-2 focus:ring-tv-accent/30"
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
                  ? "bg-tv-accent text-tv-accent-ink"
                  : "text-tv-fg-1 hover:text-tv-fg-0",
              ].join(" ")}
            >
              {it.label}
            </button>
          ))}
        </div>

        {/* Hero view settings. Only relevant to the Discover surface,
            so we hide it on the other tabs to avoid offering a control
            whose effect isn't visible. Living here (not inside the hero
            itself) keeps it reachable even when the viewer has hidden
            the spotlight entirely — the previous in-hero gear
            vanished with the hero, making "ocultar" a dead end. */}
        {tab === "discover" ? (
          <HeroSettings
            mode={heroMode}
            modeOptions={heroModeOptions}
            onModeChange={onHeroModeChange}
          />
        ) : null}
      </div>
    </header>
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
