import type { ChannelCategory } from "@/api/types";
import type { CategoryFilter } from "./CategoryChips";

/**
 * Canonical order of channel categories used across the Live TV surfaces
 * (category chips, discover rails, filters). One source of truth so that
 * changing "kids goes before entertainment" only has to happen here.
 *
 * The chips bar prepends "all"; rails only iterate real categories.
 */
export const CHANNEL_CATEGORY_ORDER: ChannelCategory[] = [
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

/** Same order with "all" prepended, for CategoryChips. */
export const CATEGORY_FILTER_ORDER: CategoryFilter[] = [
  "all",
  ...CHANNEL_CATEGORY_ORDER,
];
