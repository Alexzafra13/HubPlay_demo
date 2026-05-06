import { useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useCollection } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";
import type { StudioItem } from "@/api/types";

// /collections/:id — saga collection page (Jellyfin-style "Movie
// Collections"). Renders the saga's name + backdrop in a hero band
// with the small poster floating into it, then a grid of member
// movies in release order so a viewer can binge them sequentially
// without leaving the surface.
//
// Items array reuses StudioItem on the wire, so the same Tile
// component works here as on /studios/{slug}. The id is the canonical
// "collection:<tmdb_id>" string the backend builds; the route
// segment passes through unencoded because colons are URL-safe.

function MovieTile({ item }: { item: StudioItem }) {
  const [imageFailed, setImageFailed] = useState(false);
  const showPoster = !!item.poster_url && !imageFailed;
  return (
    <Link
      to={`/movies/${item.id}`}
      className="group flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-accent rounded-[--radius-lg]"
    >
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated ring-1 ring-border/40 transition-transform duration-300 group-hover:scale-[1.03] group-hover:ring-accent/40">
        {showPoster ? (
          <img
            src={item.poster_url}
            alt={item.title}
            loading="lazy"
            decoding="async"
            className="h-full w-full object-cover"
            onError={() => setImageFailed(true)}
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-4xl font-bold text-text-muted">
              {item.title.charAt(0).toUpperCase()}
            </span>
          </div>
        )}
      </div>
      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="line-clamp-2 text-sm font-medium text-text-primary group-hover:text-white transition-colors">
          {item.title}
        </p>
        {item.year != null && (
          <span className="text-xs text-text-muted">{item.year}</span>
        )}
      </div>
    </Link>
  );
}

export default function CollectionDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const { data: collection, isLoading, isError } = useCollection(id ?? "");

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (isError || !collection) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <EmptyState
          title={t("collectionDetail.notFoundTitle")}
          description={t("collectionDetail.notFoundDescription")}
        />
      </div>
    );
  }

  const totalCount = collection.items.length;

  return (
    <div className="flex flex-col">
      {/* Hero band — full backdrop bleed if TMDb shipped one,
          otherwise the surface gradient. The poster floats over
          the bottom of the backdrop with the saga name + count
          beside it, lifting the page from "list of movies" into
          "this is a saga". */}
      <div className="relative overflow-hidden">
        {collection.backdrop_url ? (
          <>
            <div
              aria-hidden
              className="absolute inset-0 -z-20 bg-cover bg-center"
              style={{ backgroundImage: `url(${collection.backdrop_url})` }}
            />
            <div
              aria-hidden
              className="absolute inset-0 -z-10 bg-gradient-to-b from-bg-canvas/40 via-bg-canvas/75 to-bg-canvas"
            />
          </>
        ) : (
          <div className="absolute inset-0 -z-10 bg-gradient-to-b from-bg-card/60 via-bg-card/85 to-bg-canvas" />
        )}
        <header className="flex flex-col items-center gap-6 px-6 py-12 text-center sm:flex-row sm:items-end sm:gap-8 sm:px-10 sm:py-16 sm:text-left">
          <div className="flex h-56 w-40 shrink-0 items-center justify-center overflow-hidden rounded-[--radius-lg] bg-bg-elevated ring-1 ring-border/60 shadow-xl shadow-black/40 sm:h-64 sm:w-44">
            {collection.poster_url ? (
              <img
                src={collection.poster_url}
                alt={collection.name}
                loading="eager"
                decoding="async"
                className="h-full w-full object-cover"
              />
            ) : (
              <span className="text-3xl font-bold text-text-muted">
                {collection.name.charAt(0).toUpperCase()}
              </span>
            )}
          </div>

          <div className="flex flex-col gap-3 sm:pb-2">
            <h1 className="text-3xl font-semibold text-text-primary drop-shadow-md sm:text-5xl">
              {collection.name}
            </h1>
            {totalCount > 0 && (
              <p className="text-sm text-text-secondary">
                {t("collectionDetail.titlesCount", { count: totalCount })}
              </p>
            )}
            {collection.overview && (
              <p className="max-w-2xl text-sm leading-relaxed text-text-primary/90">
                {collection.overview}
              </p>
            )}
          </div>
        </header>
      </div>

      <div className="flex flex-col gap-8 px-6 py-8 sm:px-10">
        {totalCount === 0 ? (
          <p className="text-sm text-text-muted">
            {t("collectionDetail.noItems")}
          </p>
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {collection.items.map((item) => (
              <MovieTile key={item.id} item={item} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
