import type { EPGProgram } from "@/api/types";

/** Returns the program currently airing on the channel, or null. */
export function getNowPlaying(programs: EPGProgram[] | undefined): EPGProgram | null {
  if (!programs || programs.length === 0) return null;
  const now = Date.now();
  return (
    programs.find(
      (p) =>
        new Date(p.start_time).getTime() <= now &&
        new Date(p.end_time).getTime() > now,
    ) ?? null
  );
}

/**
 * Returns the next upcoming program for a channel, or null.
 *
 * Assumes `programs` arrives ordered by start_time — the backend's
 * Schedule / BulkSchedule queries both end in `ORDER BY start_time`
 * (internal/db/epg_repository.go). No re-sort here.
 */
export function getUpNext(programs: EPGProgram[] | undefined): EPGProgram | null {
  if (!programs || programs.length === 0) return null;
  const now = Date.now();
  return (
    programs.find((p) => new Date(p.start_time).getTime() > now) ?? null
  );
}

/** Percentage (0-100) of elapsed time in the program's window. */
export function getProgramProgress(program: EPGProgram): number {
  const now = Date.now();
  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();
  const duration = end - start;
  if (duration <= 0) return 0;
  return Math.min(100, Math.max(0, ((now - start) / duration) * 100));
}

/** HH:MM local time for an ISO datetime string. */
export function formatTime(dateStr: string): string {
  return new Date(dateStr).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Capitalise the first letter of a non-empty string. Fallback used by
 * the Live TV surfaces when an i18n key is missing; proper translations
 * (CategoryChips.defaultLabel) win when available.
 */
export function capitalize(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toUpperCase() + s.slice(1);
}
