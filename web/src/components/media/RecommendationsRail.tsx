import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { useItemRecommendations } from "@/api/hooks";
import type { Recommendation } from "@/api/types";

// "More like this" rail rendered under the cast strip on the detail
// page. Each card opens either the local detail page (when the user
// already has the title) or TMDb (when they don't), so the user can
// expand their watchlist without leaving the app.
//
// The rail hides itself when the backend returned no candidates — TMDb
// has no recommendations for this title, no provider is configured,
// or the item has no TMDb match. Loading state is silent (no spinner)
// because the section is decorative; surfacing a spinner would make
// every detail page feel like it's waiting on something.
export function RecommendationsRail({ itemId }: { itemId: string }) {
  const { t } = useTranslation();
  const { data, isLoading } = useItemRecommendations(itemId);

  const items = data?.items ?? [];
  if (isLoading || items.length === 0) return null;

  return (
    <section>
      <h2 className="mb-3 text-lg font-semibold text-text-primary">
        {t("itemDetail.recommendations")}
      </h2>
      <div className="flex flex-wrap gap-4">
        {items.map((rec) => (
          <RecommendationCard key={rec.tmdb_id} rec={rec} />
        ))}
      </div>
    </section>
  );
}

function RecommendationCard({ rec }: { rec: Recommendation }) {
  const { t } = useTranslation();

  // Local hits route to /movies/{id} or /series/{id}; the
  // recommendations endpoint doesn't tell us which (TMDb returns the
  // title's type implicitly via the parent route, not on each row),
  // so we default to /movies/{id} for now. The router accepts both
  // shapes and renders ItemDetail either way; the only practical
  // difference is the URL the user sees.
  const localHref = rec.local_id ? `/movies/${rec.local_id}` : null;
  const tmdbHref = `https://www.themoviedb.org/movie/${encodeURIComponent(rec.tmdb_id)}`;

  const inner = (
    <div className="group flex w-[140px] shrink-0 flex-col gap-2">
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-md] bg-bg-elevated ring-1 ring-border/40 transition-transform duration-200 group-hover:scale-[1.04]">
        {rec.poster_url ? (
          <img
            src={rec.poster_url}
            alt={rec.title}
            loading="lazy"
            decoding="async"
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-2xl font-bold text-text-muted">
            {rec.title.charAt(0)}
          </div>
        )}
        {/* Corner badge — distinguishes "you have it" (deep-linkable)
            from "you don't" (TMDb external). Plex's "Related" rail
            does the same: a small "Available" pill on local hits. */}
        {rec.in_library ? (
          <span className="absolute left-1.5 top-1.5 rounded-full bg-accent/90 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-white shadow-sm backdrop-blur-sm">
            {t("itemDetail.recInLibrary")}
          </span>
        ) : (
          <span className="absolute left-1.5 top-1.5 rounded-full bg-black/60 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-text-secondary shadow-sm backdrop-blur-sm">
            TMDb
          </span>
        )}
      </div>
      <div className="flex flex-col gap-0.5">
        <span className="line-clamp-2 text-sm font-medium leading-snug text-text-primary group-hover:text-white transition-colors">
          {rec.title}
        </span>
        {rec.year > 0 && (
          <span className="text-xs text-text-muted">{rec.year}</span>
        )}
      </div>
    </div>
  );

  if (localHref) {
    return (
      <Link
        to={localHref}
        className="outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card rounded-[--radius-md]"
      >
        {inner}
      </Link>
    );
  }
  return (
    <a
      href={tmdbHref}
      target="_blank"
      rel="noopener noreferrer"
      className="outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card rounded-[--radius-md]"
      aria-label={t("itemDetail.openOnTmdb")}
    >
      {inner}
    </a>
  );
}
