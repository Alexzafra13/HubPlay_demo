// Helpers for asking the backend for resized image variants.
//
// Background: the image handler at `/api/v1/images/file/{id}` accepts an
// optional `?w=N` query parameter and returns a cached, on-disk thumbnail
// resized to that maximum width. Cards in lists never need the full
// poster (1000+ px wide for a TMDb w500/w1280 ingest) — a 200-300 px DOM
// element is asking for ~5x more bandwidth than it can paint. Routing
// every <img> through `thumb()` collapses that waste with one helper.
//
// External URLs (legacy rows that pre-date local ingestion) are returned
// untouched: they don't have our `?w` semantics.

const LOCAL_PATH = "/api/v1/images/file/";

/**
 * Append `?w=N` to a HubPlay-served image URL. Returns the input
 * unchanged for null/empty URLs and for any URL that doesn't point at
 * our image handler (external CDN fallbacks, blob: URLs, data: URIs).
 */
export function thumb(url: string | null | undefined, width: number): string | null {
  if (!url) return null;
  if (!url.includes(LOCAL_PATH)) return url;

  // Replace any existing `w=...` so callers can up- or down-shift the
  // requested size by passing a new value, then append a fresh one.
  const stripped = url
    .replace(/([?&])w=\d+&/g, "$1")
    .replace(/[?&]w=\d+$/g, "");
  const sep = stripped.includes("?") ? "&" : "?";
  return `${stripped}${sep}w=${width}`;
}
