import { useTranslation } from "react-i18next";
import type { ChannelCategory } from "@/api/types";

/**
 * "all" is a virtual filter that keeps every channel. It lives alongside
 * the backend's canonical `ChannelCategory` values so the chips bar can
 * model "show everything" without a separate flag.
 */
export type CategoryFilter = ChannelCategory | "all";

interface CategoryChipsProps {
  /** Category → count, as computed from the loaded channel list. */
  counts: Record<CategoryFilter, number>;
  active: CategoryFilter;
  onChange: (next: CategoryFilter) => void;
  /**
   * Display order. Categories with a zero count are hidden so the bar
   * stays tight on libraries that only carry a subset of categories.
   */
  order?: CategoryFilter[];
}

/**
 * categoryAccent maps each canonical category to a fixed swatch color.
 * These are UI-only — the canonical category name is what drives data.
 * Colors are chosen so neighbors don't clash and so the palette reads
 * well on the dark TV theme background.
 */
const categoryAccent: Record<CategoryFilter, string> = {
  all: "var(--tv-accent)",
  general: "#7aa2ff",
  news: "#ff6b78",
  sports: "#6be2a8",
  movies: "#ffb84a",
  music: "#c99bff",
  entertainment: "#8ee3c8",
  kids: "#ffd84a",
  culture: "#d8a36a",
  documentaries: "#7ed6ff",
  international: "#9cb4ff",
  travel: "#e36aa3",
  religion: "#b19cd9",
  adult: "#e74c3c",
};

const defaultOrder: CategoryFilter[] = [
  "all",
  "news",
  "sports",
  "movies",
  "music",
  "entertainment",
  "documentaries",
  "kids",
  "culture",
  "international",
  "travel",
  "religion",
  "general",
  "adult",
];

export function CategoryChips({
  counts,
  active,
  onChange,
  order = defaultOrder,
}: CategoryChipsProps) {
  const { t } = useTranslation();

  return (
    <div
      className="flex gap-2 overflow-x-auto pb-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      role="tablist"
      aria-label={t("liveTV.categoriesAria", { defaultValue: "Categorías" })}
    >
      {order.map((cat) => {
        const count = counts[cat] ?? 0;
        if (cat !== "all" && count === 0) return null;

        const isActive = active === cat;
        return (
          <button
            key={cat}
            type="button"
            role="tab"
            aria-selected={isActive}
            onClick={() => onChange(cat)}
            className={[
              "flex shrink-0 items-center gap-2 rounded-full border px-3.5 py-1.5 text-xs font-medium transition-colors",
              isActive
                ? "border-tv-accent/60 bg-tv-accent/[0.12] text-tv-fg-0"
                : "border-tv-line bg-tv-bg-1 text-tv-fg-1 hover:bg-tv-bg-2",
            ].join(" ")}
          >
            <span
              className="h-1.5 w-1.5 rounded-full"
              style={{
                backgroundColor: categoryAccent[cat],
                boxShadow:
                  cat === "all" ? `0 0 6px ${categoryAccent[cat]}` : undefined,
              }}
              aria-hidden="true"
            />
            <span>
              {t(`liveTV.category.${cat}`, {
                defaultValue: defaultLabel(cat),
              })}
            </span>
            <span className="font-mono text-[10px] tabular-nums text-tv-fg-3">
              {count}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/**
 * defaultLabel provides Spanish fallbacks that match the diseño/ prototype
 * when i18n keys are missing. The app's real i18n files should supersede
 * these.
 */
function defaultLabel(cat: CategoryFilter): string {
  switch (cat) {
    case "all":
      return "Todos";
    case "general":
      return "General";
    case "news":
      return "Informativos";
    case "sports":
      return "Deportes";
    case "movies":
      return "Películas";
    case "music":
      return "Música";
    case "entertainment":
      return "Entretenimiento";
    case "kids":
      return "Infantiles";
    case "culture":
      return "Cultura";
    case "documentaries":
      return "Documentales";
    case "international":
      return "Internacional";
    case "travel":
      return "Viajes";
    case "religion":
      return "Religión";
    case "adult":
      return "Adultos";
  }
}
