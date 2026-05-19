import { useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { ImageIcon } from "lucide-react";
import { useCollection } from "@/api/hooks";
import { Button, Spinner, EmptyState } from "@/components/common";
import { CollectionImageEditor } from "@/components/CollectionImageEditor";
import { useAuthStore } from "@/store/auth";
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
  const isAdmin = useAuthStore((s) => s.user?.role === "admin");
  const [imagesOpen, setImagesOpen] = useState(false);

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
  // Rango de años útil como meta: "1981 – 2008" da idea de la
  // longevidad de la saga de un vistazo. Si la saga sólo tiene una
  // peli o un único año, mostramos el año sólo.
  const years = collection.items
    .map((i) => i.year)
    .filter((y): y is number => typeof y === "number" && y > 0);
  const yearRange = years.length > 0
    ? years.length === 1 || Math.min(...years) === Math.max(...years)
      ? String(Math.min(...years))
      : `${Math.min(...years)} – ${Math.max(...years)}`
    : null;

  return (
    <div className="flex flex-col">
      {/* Hero band — full-bleed backdrop con altura fija mínima para
          que la saga "respire". Sin overview ni tráiler (Plex/Jellyfin
          en colecciones tampoco los enseñan — la portada + el grid
          son el sujeto de la página). El hero combina:
            · backdrop como bg (o un fallback de color que sale del
              poster_color cuando TMDb no provee backdrop)
            · doble gradient: oscuro arriba (legibilidad de la topbar)
              + oscuro abajo (legibilidad del título)
            · poster grande con ring sutil y sombra para que se sienta
              flotando sobre la imagen
            · h1 con un peso más fuerte y tracking ajustado
            · meta-pill con el año-range y el count */}
      <div
        className="relative overflow-hidden"
        style={{
          minHeight: "clamp(360px, 50vh, 520px)",
        }}
      >
        {collection.backdrop_url ? (
          <div
            aria-hidden
            className="absolute inset-0 -z-20 bg-cover bg-center"
            style={{ backgroundImage: `url(${collection.backdrop_url})` }}
          />
        ) : (
          // Fallback cuando TMDb no provee backdrop — un gradient
          // suave con el accent del proyecto en lugar de un gris
          // plano. Más vivo y menos "sala de espera" que el card
          // shade que había antes.
          <div
            aria-hidden
            className="absolute inset-0 -z-20 bg-gradient-to-br from-accent/15 via-bg-card to-bg-canvas"
          />
        )}
        {/* Gradient top + bottom para legibilidad en ambos extremos.
            El de arriba evita que la topbar se pierda sobre un backdrop
            claro; el de abajo asegura que el título destaque. */}
        <div
          aria-hidden
          className="absolute inset-x-0 top-0 -z-10 h-32 bg-gradient-to-b from-bg-canvas/80 to-transparent"
        />
        <div
          aria-hidden
          className="absolute inset-x-0 bottom-0 -z-10 h-64 bg-gradient-to-t from-bg-canvas via-bg-canvas/70 to-transparent"
        />

        {/* Admin shortcut — esquina superior derecha del hero. Sólo
            visible para admin; abre el editor de imágenes (póster +
            fondo, URL externa o subida desde disco). */}
        {isAdmin && (
          <div className="absolute right-4 top-4 z-10 sm:right-8 sm:top-6">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setImagesOpen(true)}
              className="bg-black/50 text-white backdrop-blur hover:bg-black/70"
            >
              <ImageIcon className="h-3.5 w-3.5" />
              {t("collectionImage.menuLabel", {
                defaultValue: "Cambiar imágenes",
              })}
            </Button>
          </div>
        )}

        <header className="relative flex flex-col items-center gap-8 px-6 pt-12 pb-10 text-center sm:flex-row sm:items-end sm:gap-10 sm:px-10 sm:pt-16 sm:pb-12 sm:text-left">
          <div className="flex h-64 w-44 shrink-0 items-center justify-center overflow-hidden rounded-[--radius-lg] bg-bg-elevated ring-1 ring-white/10 shadow-2xl shadow-black/60 sm:h-80 sm:w-56">
            {collection.poster_url ? (
              <img
                src={collection.poster_url}
                alt={collection.name}
                loading="eager"
                decoding="async"
                className="h-full w-full object-cover"
              />
            ) : (
              <span className="text-5xl font-bold text-text-muted">
                {collection.name.charAt(0).toUpperCase()}
              </span>
            )}
          </div>

          <div className="flex flex-col gap-3 sm:pb-3">
            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-accent/90">
              {t("collectionDetail.kind", { defaultValue: "Saga" })}
            </p>
            <h1 className="text-3xl font-bold leading-tight tracking-tight text-text-primary drop-shadow-md sm:text-5xl">
              {collection.name}
            </h1>
            {(totalCount > 0 || yearRange) && (
              <div className="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 text-sm text-text-secondary sm:justify-start">
                {totalCount > 0 && (
                  <span className="font-medium">
                    {t("collectionDetail.titlesCount", { count: totalCount })}
                  </span>
                )}
                {totalCount > 0 && yearRange && (
                  <span className="text-text-muted/60">·</span>
                )}
                {yearRange && (
                  <span className="font-mono text-text-secondary">
                    {yearRange}
                  </span>
                )}
              </div>
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

      {/* Editor de imágenes (admin) — el botón del hero lo dispara.
          Se monta sólo cuando el operador lo abre para no consumir
          render gratis en cada visita a la página. */}
      {isAdmin && imagesOpen && (
        <CollectionImageEditor
          isOpen={imagesOpen}
          onClose={() => setImagesOpen(false)}
          collection={collection}
        />
      )}
    </div>
  );
}
