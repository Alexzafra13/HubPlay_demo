// Pure helpers used by hero surfaces. Lives in /utils so the
// component file (./components/media/heroMeta.tsx) can stay
// component-only — the `react-refresh/only-export-components` rule
// breaks Fast Refresh when a file mixes components and module
// constants/functions.

/**
 * Format a premiere/air date as a localised "25 may 2018" string.
 * Returns null when the input is missing or not a valid date so callers
 * can fall through to a year-only chip.
 */
export function formatPremiereDate(
  date: string | null | undefined,
  language: string,
): string | null {
  if (!date) return null;
  const d = new Date(date);
  if (Number.isNaN(d.getTime())) return null;
  return d.toLocaleDateString(language, {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}
