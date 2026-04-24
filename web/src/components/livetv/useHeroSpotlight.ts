import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { useUserPreference } from "@/api/hooks";
import {
  type HeroMode,
  type HeroModeOption,
} from "./HeroSettings";
import type { HeroSpotlightItem } from "./HeroSpotlight";
import { getNowPlaying } from "./epgHelpers";

const HERO_MODE_PREF_KEY = "livetv.hero_mode";
const HERO_ITEM_LIMIT = 6;

interface UseHeroSpotlightArgs {
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  favoriteSet: Set<string>;
}

interface UseHeroSpotlightResult {
  items: HeroSpotlightItem[];
  label: string;
  /** What the user picked. Persisted across devices. */
  mode: HeroMode;
  setMode: (mode: HeroMode) => void;
  /** Menu options for the HeroSettings dropdown. */
  modeOptions: HeroModeOption[];
}

/**
 * useHeroSpotlight drives the "Discover" hero tile on the Live TV page.
 *
 * Centralises three concerns that used to be inlined in LiveTV.tsx:
 *
 *   1. Preference persistence (`livetv.hero_mode` in user_preferences).
 *   2. Fallback chain — if the user picked "favorites" but has none, we
 *      silently resolve to live-now → newest so the spotlight stays
 *      populated. The returned `label` reflects the mode that ACTUALLY
 *      rendered so nothing on-screen lies.
 *   3. Mode-option translations for the HeroSettings dropdown.
 *
 * The hook is pure-ish (it owns a preference mutation) and keeps the
 * page component focused on layout.
 */
export function useHeroSpotlight({
  channels,
  scheduleByChannel,
  favoriteSet,
}: UseHeroSpotlightArgs): UseHeroSpotlightResult {
  const { t } = useTranslation();
  const [mode, setMode] = useUserPreference<HeroMode>(
    HERO_MODE_PREF_KEY,
    "favorites",
  );

  // Compute a pool for each signal up-front so the fallback below can
  // pick whichever one has content without recomputing.
  const pools = useMemo(() => {
    const favorites = channels.filter((c) => favoriteSet.has(c.id));
    const liveNow = channels
      .filter((c) => getNowPlaying(scheduleByChannel[c.id]))
      .slice()
      .sort((a, b) => a.number - b.number);
    const newest = channels
      .slice()
      .sort((a, b) => (b.added_at ?? "").localeCompare(a.added_at ?? ""));
    return { favorites, liveNow, newest };
  }, [channels, favoriteSet, scheduleByChannel]);

  // Resolve the mode actually rendered. "off" is honoured as-is. Any
  // other mode falls through favorites → live-now → newest until a
  // non-empty pool is found, so a fresh account without favorites still
  // lands on a populated spotlight.
  const effectiveMode = useMemo<HeroMode>(() => {
    if (mode === "off") return "off";
    const order: HeroMode[] =
      mode === "favorites"
        ? ["favorites", "live-now", "newest"]
        : mode === "live-now"
          ? ["live-now", "favorites", "newest"]
          : ["newest", "favorites", "live-now"];
    for (const m of order) {
      const pool =
        m === "favorites"
          ? pools.favorites
          : m === "live-now"
            ? pools.liveNow
            : pools.newest;
      if (pool.length > 0) return m;
    }
    return mode;
  }, [mode, pools]);

  const items = useMemo<HeroSpotlightItem[]>(() => {
    if (effectiveMode === "off") return [];
    const pool =
      effectiveMode === "favorites"
        ? pools.favorites
        : effectiveMode === "live-now"
          ? pools.liveNow
          : pools.newest;
    return pool.slice(0, HERO_ITEM_LIMIT).map((c) => ({
      channel: c,
      nowPlaying: getNowPlaying(scheduleByChannel[c.id]),
    }));
  }, [effectiveMode, pools, scheduleByChannel]);

  const label = useMemo(() => {
    switch (effectiveMode) {
      case "favorites":
        return t("liveTV.hero.label.favorites", { defaultValue: "Tu favorito" });
      case "live-now":
        return t("liveTV.hero.label.liveNow", {
          defaultValue: "En directo ahora",
        });
      case "newest":
        return t("liveTV.hero.label.newest", {
          defaultValue: "Recién añadidos",
        });
      default:
        return "";
    }
  }, [effectiveMode, t]);

  const modeOptions = useMemo<HeroModeOption[]>(
    () => [
      {
        mode: "favorites",
        label: t("liveTV.hero.option.favorites", {
          defaultValue: "Mis favoritos",
        }),
        hint: t("liveTV.hero.option.favoritesHint", {
          defaultValue: "Los canales con ♥, rotando.",
        }),
      },
      {
        mode: "live-now",
        label: t("liveTV.hero.option.liveNow", {
          defaultValue: "En vivo ahora",
        }),
        hint: t("liveTV.hero.option.liveNowHint", {
          defaultValue: "Canales con programa EPG emitiendo ahora mismo.",
        }),
      },
      {
        mode: "newest",
        label: t("liveTV.hero.option.newest", {
          defaultValue: "Recién añadidos",
        }),
        hint: t("liveTV.hero.option.newestHint", {
          defaultValue: "Los últimos canales que entraron en la biblioteca.",
        }),
      },
      {
        mode: "off",
        label: t("liveTV.hero.option.off", {
          defaultValue: "Ocultar destacado",
        }),
        hint: t("liveTV.hero.option.offHint", {
          defaultValue: "Discover empieza directamente por las rails.",
        }),
      },
    ],
    [t],
  );

  return { items, label, mode, setMode, modeOptions };
}
