import { useTranslation } from "react-i18next";
import {
  HeroSettings,
  type HeroMode,
  type HeroModeOption,
} from "./HeroSettings";

export type ViewTab = "now" | "discover" | "guide" | "favorites";

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
}: LiveTvTopBarProps) {
  const { t } = useTranslation();
  // Tab order encodes intent: "Ahora" first because it's the answer to
  // the user's actual question on landing ("what do I put on?"), then
  // "Descubrir" for browsing the lineup, "Guía" for the schedule view
  // and "Favoritos" as the personal anchor list.
  const tabs: { id: ViewTab; label: string }[] = [
    { id: "now", label: t("liveTV.tab.now", { defaultValue: "Ahora" }) },
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

      {/* Hero view settings. Only relevant to the Discover surface,
          so we hide it on the other tabs to avoid offering a control
          whose effect isn't visible. Living here (not inside the hero
          itself) keeps it reachable even when the viewer has hidden
          the spotlight entirely — the previous in-hero gear vanished
          with the hero, making "ocultar" a dead end. */}
      {tab === "discover" ? (
        <HeroSettings
          mode={heroMode}
          modeOptions={heroModeOptions}
          onModeChange={onHeroModeChange}
        />
      ) : null}
    </div>
  );

  // On the discover surface the title + counters are lifted into the
  // hero's top-left overlay (DiscoverView wires that), so this header
  // shrinks to just the controls strip — no double title.
  // Header copy adapts to the active tab so the title + subtitle
  // describe the surface you're actually looking at — instead of
  // claiming "169 canales" while the page only shows 45 (the old
  // behaviour was technically correct for the library, but lied
  // about what was on screen).
  const headerCopy = (() => {
    switch (tab) {
      case "now":
        return {
          title: t("liveTV.title.now", { defaultValue: "Ahora en directo" }),
          subtitle: (
            <>
              <b className="text-tv-fg-1">{liveNow}</b>{" "}
              {t("liveTV.channelsLive", {
                defaultValue: "canales emitiendo ahora",
              })}
            </>
          ),
        };
      case "guide":
        return {
          title: t("liveTV.title.guide", { defaultValue: "Guía TV" }),
          subtitle: (
            <>
              <b className="text-tv-fg-1">{totalChannels}</b>{" "}
              {t("liveTV.channels", { defaultValue: "canales" })}
            </>
          ),
        };
      case "favorites":
        return {
          title: t("liveTV.title.favorites", {
            defaultValue: "Tus favoritos",
          }),
          subtitle: null,
        };
      case "discover":
      default:
        return {
          title: t("liveTV.title", { defaultValue: "TV en directo" }),
          subtitle: (
            <>
              <b className="text-tv-fg-1">{totalChannels}</b>{" "}
              {t("liveTV.channels", { defaultValue: "canales" })} ·{" "}
              <b className="text-tv-fg-1">{liveNow}</b>{" "}
              {t("liveTV.liveNow", { defaultValue: "en vivo ahora" })}
            </>
          ),
        };
    }
  })();

  return (
    <header className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      {tab !== "discover" && (
        <div>
          <h1 className="flex items-center gap-2 text-xl font-bold text-tv-fg-0 md:text-2xl">
            <span className="inline-flex h-2.5 w-2.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
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
