// Adapter from the federation wire shape (FederationRemoteItem) to
// the local MediaItem shape consumed by PosterCard, MediaGrid, and
// the rest of the browse UI.
//
// We deliberately want federated items to render with the SAME
// component as local items (Plex-style mixed surfacing): a peer's
// library appears as a sidebar entry, and clicking through opens a
// grid that looks identical to /movies but with poster URLs proxied
// through our origin and a small per-card "shared by Pedro" badge.
//
// MediaItem is the canonical browse shape; this file is the only
// place we should be filling its fields with default-or-null values
// for the federation case. Anything else that ends up needing the
// canonical shape (e.g. a future federated home rail) should reuse
// this adapter rather than spreading the conversion ad hoc.

import type { FederationRemoteItem, MediaItem, MediaType } from "./types";

const KNOWN_TYPES = new Set<MediaType>(["movie", "series", "season", "episode"]);

// federationItemToMediaItem turns a peer-returned item into a MediaItem
// PosterCard can consume directly. Fields the federation wire format
// doesn't carry (poster_color, blurhash, genres, ratings, ...) come
// out as null / undefined / empty array — the card handles each of
// those gracefully (placeholder gradient when no poster_color, no
// rating chip when community_rating is null, etc.).
//
// `type` is narrowed to the MediaItem union when the wire string is
// one of the known values; otherwise it falls back to `"movie"` so
// the card's link-builder still produces a valid path. Unknown types
// shouldn't happen against a well-behaved peer — if they do we'd
// rather render a card with the wrong header link than crash the
// grid mid-render.
export function federationItemToMediaItem(it: FederationRemoteItem): MediaItem {
  const narrowedType: MediaType = KNOWN_TYPES.has(it.type as MediaType)
    ? (it.type as MediaType)
    : "movie";
  return {
    id: it.id,
    type: narrowedType,
    title: it.title,
    original_title: null,
    year: it.year ?? null,
    sort_title: it.title.toLowerCase(),
    overview: it.overview ?? null,
    tagline: null,
    genres: [],
    community_rating: null,
    content_rating: null,
    duration_ticks: null,
    premiere_date: null,
    poster_url: it.poster_url ?? null,
    backdrop_url: null,
    logo_url: null,
    parent_id: null,
    series_id: null,
    season_number: null,
    episode_number: null,
    path: null,
    // Forward the peer's pre-extracted swatches so the aurora canvas
    // paints on first render. Same field name + shape MediaItem
    // already uses for local items; downstream code (ItemDetail's
    // hasServerPalette check, aurora.ts) is shape-agnostic.
    backdrop_colors: it.backdrop_colors,
  };
}
