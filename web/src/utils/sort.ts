import type { MediaItem } from "@/api/types";

export type SortOption = "title" | "year" | "added" | "rating";

export const SORT_OPTIONS: { value: SortOption; label: string }[] = [
  { value: "title", label: "Title" },
  { value: "year", label: "Year" },
  { value: "added", label: "Recently Added" },
  { value: "rating", label: "Rating" },
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
