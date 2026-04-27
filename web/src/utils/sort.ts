import type { MediaItem } from "@/api/types";

export type SortOption = "title" | "year" | "added" | "rating";

/**
 * Sort options as (value, i18n key) pairs. The label is resolved at
 * render time via i18next so the dropdown follows the active locale.
 * Keep the order stable — it doubles as the dropdown order.
 */
export const SORT_OPTIONS: { value: SortOption; labelKey: string }[] = [
  { value: "title", labelKey: "sort.title" },
  { value: "year", labelKey: "sort.year" },
  { value: "added", labelKey: "sort.added" },
  { value: "rating", labelKey: "sort.rating" },
];

export function sortItems(items: MediaItem[], sort: SortOption): MediaItem[] {
  const sorted = [...items];
  switch (sort) {
    case "title":
      return sorted.sort((a, b) => a.sort_title.localeCompare(b.sort_title));
    case "year":
      return sorted.sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
    case "added":
      return sorted; // API default order is newest first
    case "rating":
      return sorted.sort(
        (a, b) => (b.community_rating ?? 0) - (a.community_rating ?? 0),
      );
  }
}
