import { useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { usePerson } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";

// /people/:id — actor / director / writer landing.
//
// Server contract (handlers/people.go::Get):
//   { id, name, type, image_url?, filmography:[{item_id, type, title,
//     year?, role, character?, sort_order}] }
//
// The page is read-only and intentionally lightweight. Filmography is
// pre-deduped + ordered by the repo (lowest sort_order wins per
// item), so we don't re-sort here. The grid is plain Tailwind — same
// 5/4/3/2-column rhythm the Movies and Series surfaces use, kept
// inline because PersonDetail is the only place that consumes this
// shape and lifting to a shared `<MediaGrid>` would require widening
// MediaGrid's type to the FilmographyEntry contract.

function FilmographyTile({
  itemId,
  title,
  year,
  character,
}: {
  itemId: string;
  title: string;
  year?: number;
  character?: string;
}) {
  const { t } = useTranslation();
  return (
    <Link
      to={`/items/${itemId}`}
      className="group flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-accent rounded-[--radius-lg]"
    >
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated transition-transform duration-300 group-hover:scale-[1.03]">
        {/* Placeholder tile — the filmography wire intentionally stays
            slim (id + title + year + role) and does NOT include poster
            URLs. Surfacing the poster here would require a per-item
            join the user can already get one click away on the item
            detail page; keeping the wire small means a 100-credit
            actor page is one query, not 100. */}
        <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
          <span className="text-4xl font-bold text-text-muted">
            {title.charAt(0).toUpperCase()}
          </span>
        </div>
      </div>
      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="line-clamp-2 text-sm font-medium text-text-primary group-hover:text-white transition-colors">
          {title}
        </p>
        <div className="flex flex-wrap items-center gap-1.5 text-xs text-text-muted">
          {year != null && <span>{year}</span>}
          {character && (
            <>
              {year != null && <span className="text-text-muted/40">·</span>}
              <span className="truncate">
                {t("personDetail.asCharacter", { character })}
              </span>
            </>
          )}
        </div>
      </div>
    </Link>
  );
}

export default function PersonDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const { data: person, isLoading, isError } = usePerson(id ?? "");
  // Track image-load failure separately from absence — the wire's
  // image_url is only present when the thumb file is on disk under
  // imageDir, but the network can still fail (cache eviction, route
  // glitch). On error we fall back to the same initial-letter circle
  // CastChip uses, keeping the page coherent with where the user
  // came from.
  const [photoFailed, setPhotoFailed] = useState(false);

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (isError || !person) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <EmptyState
          title={t("personDetail.notFoundTitle")}
          description={t("personDetail.notFoundDescription")}
        />
      </div>
    );
  }

  const showPhoto = !!person.image_url && !photoFailed;

  return (
    <div className="flex flex-col gap-8 px-6 py-8 sm:px-10">
      <header className="flex items-center gap-6">
        <div className="flex h-32 w-32 shrink-0 items-center justify-center overflow-hidden rounded-full bg-bg-elevated text-4xl font-bold text-text-muted ring-1 ring-border/40 sm:h-40 sm:w-40">
          {showPhoto ? (
            <img
              src={person.image_url}
              alt={person.name}
              loading="eager"
              decoding="async"
              width={160}
              height={160}
              className="h-full w-full object-cover"
              onError={() => setPhotoFailed(true)}
            />
          ) : (
            person.name.charAt(0)
          )}
        </div>
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold text-text-primary sm:text-3xl">
            {person.name}
          </h1>
          {person.type && (
            <span className="text-sm capitalize text-text-muted">
              {person.type}
            </span>
          )}
        </div>
      </header>

      <section>
        <h2 className="mb-4 text-lg font-semibold text-text-primary">
          {t("personDetail.filmography")}
        </h2>
        {person.filmography.length === 0 ? (
          <p className="text-sm text-text-muted">
            {t("personDetail.noFilmography")}
          </p>
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {person.filmography.map((entry) => (
              <FilmographyTile
                key={entry.item_id}
                itemId={entry.item_id}
                title={entry.title}
                year={entry.year}
                character={entry.character}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
