// Pure helpers used by LibrariesAdmin and its sub-components.
// Constants live in `./constants.ts`; this file is for functions only
// so a future port (server-side validation, CLI tool) can import the
// data tables without dragging the React tree along.

import type { Library } from "@/api/types";

export function scanStatusVariant(status: string) {
  switch (status) {
    case "scanning":
      return "warning" as const;
    case "error":
      return "error" as const;
    default:
      return "success" as const;
  }
}

// originLabel returns the secondary identity of a library: the M3U host
// for IPTV, the first filesystem path for media. Truncated on purpose —
// the full value lives in the tooltip (originTitle).
export function originLabel(lib: Library): string {
  if (lib.content_type === "livetv") {
    if (!lib.m3u_url) return "";
    try {
      return new URL(lib.m3u_url).host;
    } catch {
      return lib.m3u_url;
    }
  }
  return (lib.paths ?? [])[0] ?? "";
}

export function originTitle(lib: Library): string {
  if (lib.content_type === "livetv") return lib.m3u_url ?? "";
  return (lib.paths ?? []).join(", ");
}
