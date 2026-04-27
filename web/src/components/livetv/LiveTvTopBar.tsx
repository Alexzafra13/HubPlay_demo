import { useTranslation } from "react-i18next";
import { useTopBarSlot } from "@/components/layout/TopBarSlot";
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
 * Layout split, so the user gets a single sticky bar instead of two:
 *   - Title block (h1 + counts) renders inline at the top of the page
 *     and scrolls away with the content. It's not a control surface,
 *     so it doesn't need to follow the viewer.
 *   - Search + tab switcher + hero-mode picker get hoisted into the
 *     global TopBar via `useTopBarSlot` — that bar is already sticky
 *     with a frosted-glass treatment, so the controls inherit it for
 *     free and the viewer doesn't see two stacked headers.
 *
 * If no slot provider is available (e.g. unit tests rendering this
 * component standalone), the controls fall back to rendering inline
 * so the component is still testable in isolation.
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

  // Try to hoist into the global TopBar slot. If the provider is
  // missing (test harness, custom shell), render inline as a fallback.
  const slotActive = useTopBarSlot(controls);

  // On the discover surface we lift the title + counters into the
  // hero's top-left overlay (DiscoverView wires that), so this
  // component renders nothing in that case — the controls already live
  // in the global TopBar via the slot. The fallback path (no provider,
  // e.g. unit tests) and the guide/favorites tabs still render the
  // inline header so they keep their identity strip.
  if (slotActive && tab === "discover") {
    return null;
  }

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
      <div>
        <h1 className="flex items-center gap-2 text-xl font-bold text-tv-fg-0 md:text-2xl">
          <span className="inline-flex h-2.5 w-2.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
          {headerCopy.title}
        </h1>
        {headerCopy.subtitle ? (
          <p className="mt-1 text-xs text-tv-fg-2">{headerCopy.subtitle}</p>
        ) : null}
      </div>

      {!slotActive && controls}
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
