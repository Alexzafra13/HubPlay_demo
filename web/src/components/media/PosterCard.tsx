import { memo, useState } from "react";
import { Link } from "react-router";
import type { FC, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { thumb } from "@/utils/imageUrl";
import { BlurhashPlaceholder } from "@/components/common/BlurhashPlaceholder";
import { ItemKebab } from "./ItemKebab";

// DOM size of a poster card on lg screens is ~220px wide; double for
// HiDPI / Retina so the served file still has detail under 2x scale.
const POSTER_THUMB_WIDTH = 480;

interface PosterCardProps {
  item: MediaItem;
  /**
   * Optional explicit progress override (0..100). When omitted, the
   * card derives progress from `item.user_data.progress.percentage`.
   * Callers that already have a separate progress source (e.g. the
   * "Continue watching" rail joined against another query) pass it
   * explicitly to avoid round-tripping through the item.
   */
  progress?: number;
  /**
   * Optional explicit destination href. Default is `/movies/{id}` for
   * `type === 'series'` falls through to `/series/{id}`. Federated
   * grids pass `/peers/{peerID}/items/{itemID}` so the same card
   * routes into the per-peer detail page instead of the local one.
   */
  href?: string;
  /**
   * Optional badge node rendered at the bottom-left of the poster.
   * Used by federated grids to attribute the source peer ("Pedro",
   * "Maria") so the user can tell at a glance whether the card is
   * local or remote without changing the rest of the layout.
   */
  cornerBadge?: ReactNode;
  onClick?: () => void;
}

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

const PosterCard: FC<PosterCardProps> = memo(({ item, progress, href, cornerBadge, onClick }) => {
  const { t } = useTranslation();
  const resolvedHref =
    href ?? (item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`);

  const ud = item.user_data;
  const watched = ud?.played === true;
  // The progress bar should only show partial state. If the item is
  // already played, the check overlay communicates state; rendering
  // both at once is visual noise.
  const effectiveProgress =
    progress ?? (!watched ? ud?.progress?.percentage : undefined);

  // Track when the real <img> finishes decoding so the BlurHash layer
  // fades out. Default true (already-loaded) when there's no blurhash
  // to fade — the conditional render below means the layer never
  // mounts, so the flag is irrelevant; we only flip it for the cases
  // it matters.
  const [imageLoaded, setImageLoaded] = useState(false);

  return (
    <Link
      to={resolvedHref}
      onClick={onClick}
      data-testid="poster-card"
      className="group flex flex-col outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card rounded-[--radius-lg]"
    >
      {/* Poster image stack:
          1. Wrapper tinted con el dominant colour pre-calculado.
          2. BlurHash placeholder mientras el <img> decodifica.
          3. Real <img>. El wrapper escala + el ring cambia de border
             a accent en hover — mismo "feel" que las cards de
             /collections, que el usuario notó como más vivo que
             "sólo agrandar la imagen". El ring es lo que hace que
             la card parezca responder en su totalidad. */}
      <div
        className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated ring-1 ring-border/40 transition-all duration-300 group-hover:scale-[1.03] group-hover:ring-accent/40"
        style={item.poster_color ? { backgroundColor: item.poster_color } : undefined}
      >
        {item.poster_blurhash && !imageLoaded && (
          <BlurhashPlaceholder
            hash={item.poster_blurhash}
            className="absolute inset-0 size-full object-cover transition-opacity duration-300"
          />
        )}
        {item.poster_url ? (
          <img
            src={thumb(item.poster_url, POSTER_THUMB_WIDTH) ?? item.poster_url}
            alt={`${item.title} poster`}
            loading="lazy"
            decoding="async"
            onLoad={() => setImageLoaded(true)}
            className="relative size-full object-cover"
          />
        ) : (
          <div className="flex size-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-4xl font-bold text-text-muted">
              {item.title.charAt(0).toUpperCase()}
            </span>
          </div>
        )}

        {/* Hover: subtle play icon, no text overlay */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors duration-200 group-hover:bg-black/30">
          <div className="flex size-11 items-center justify-center rounded-full bg-white/20 backdrop-blur-sm opacity-0 scale-90 transition-all duration-200 group-hover:opacity-100 group-hover:scale-100">
            <svg
              className="size-5 text-white ml-0.5"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
        </div>

        {/* Admin kebab — top right (encima del rating cuando ambos
            existen; el kebab sólo aparece en hover/focus, así que no
            pelean en estado idle). Sólo admin lo ve.
            detailHref = la misma URL del card para que "Información del
            archivo" navegue al detalle con anchor a la sección. */}
        <div className="absolute top-2 right-2 z-10">
          <ItemKebab itemID={item.id} itemType={item.type} detailHref={resolvedHref} />
        </div>

        {/* Rating badge — bottom-left del thumbnail. Antes vivía arriba-
            derecha pero ese sitio es ahora del kebab admin (que sale
            en hover). Bottom-left va alineado con la barra de
            progreso cuando ésta existe (progress es full-width, 1px
            alto, así que no pelean). */}
        {item.community_rating != null && (
          <div className="absolute bottom-2 left-2 flex items-center gap-1 rounded-[--radius-sm] bg-black/70 backdrop-blur-sm px-1.5 py-0.5 text-[11px] font-semibold text-white">
            <svg className="size-2.5 text-warning" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
            </svg>
            {formatRating(item.community_rating)}
          </div>
        )}

        {/* Watched badge — top left. Mutually exclusive with the progress
            bar below: once played we only show the check; resuming wipes
            ud.played server-side via mark-unplayed. */}
        {watched && (
          <div
            className="absolute top-2 left-2 flex size-6 items-center justify-center rounded-full bg-accent text-white shadow-md shadow-black/40"
            aria-label={t("posterCard.watched", { defaultValue: "Watched" })}
            title={t("posterCard.watched", { defaultValue: "Watched" })}
          >
            <svg className="size-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={3}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
            </svg>
          </div>
        )}

        {/* New-episodes badge — top left, below the watched check
            (which won't ever co-render: an unwatched series with new
            episodes lacks a `played` mark). Surfaces the per-series
            episode-activity count emitted by `/items/latest?type=
            series` for shows libraries — the "Mr Robot got 3 new
            episodes this week" hint Plex / Jellyfin both ship. The
            badge stays compact at one short line so it never fights
            the rating chip on the right. */}
        {!watched && item.new_episodes_count != null && item.new_episodes_count > 0 && (
          <div
            className="absolute top-2 left-2 flex items-center gap-1 rounded-full bg-accent px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white shadow-md shadow-black/40"
            aria-label={t("posterCard.newEpisodes", {
              count: item.new_episodes_count,
              defaultValue: "{{count}} new episodes",
            })}
            title={t("posterCard.newEpisodes", {
              count: item.new_episodes_count,
              defaultValue: "{{count}} new episodes",
            })}
          >
            <span>+{item.new_episodes_count}</span>
            <span>{t("posterCard.newShort", { defaultValue: "new" })}</span>
          </div>
        )}

        {/* Source attribution badge — bottom-left. Federated grids
            pass a peer-name pill here so the user can tell at a
            glance which server a card came from. Local rails leave
            the prop unset and the badge isn't rendered. */}
        {cornerBadge && (
          <div className="absolute bottom-2 left-2 max-w-[80%]">
            {cornerBadge}
          </div>
        )}

        {/* Progress bar */}
        {effectiveProgress != null && effectiveProgress > 0 && (
          <div
            className="absolute bottom-0 left-0 right-0 h-1 bg-black/40"
            role="progressbar"
            aria-valuenow={Math.round(effectiveProgress)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={t("posterCard.inProgress", { defaultValue: "In progress" })}
          >
            <div
              className="h-full bg-accent transition-all duration-300"
              style={{ width: `${Math.min(100, Math.max(0, effectiveProgress))}%` }}
            />
          </div>
        )}
      </div>

      {/* Info below — clean, no overlap */}
      <div className="flex flex-col gap-0.5 pt-2 px-0.5">
        <p className="truncate text-sm font-medium text-text-primary group-hover:text-white transition-colors">
          {item.title}
        </p>
        <div className="flex items-center gap-1.5 text-xs text-text-muted">
          {item.year != null && <span>{item.year}</span>}
          {item.genres?.length > 0 && (
            <>
              {item.year != null && <span className="text-text-muted/40">·</span>}
              <span className="truncate">{item.genres.slice(0, 2).join(", ")}</span>
            </>
          )}
        </div>
      </div>
    </Link>
  );
});

PosterCard.displayName = "PosterCard";

export { PosterCard };
