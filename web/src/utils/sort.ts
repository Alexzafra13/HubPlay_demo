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
