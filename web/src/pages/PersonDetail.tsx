import { useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { usePerson } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";
import type { FilmographyEntry } from "@/api/types";

// /people/:id — actor / director / writer landing.
//
// Server contract (handlers/people.go::Get):
//   { id, name, type, image_url?, filmography:[{item_id, type, title,
//     year?, role, character?, sort_order, poster_url?}] }
//
// Layout principles:
//   1. Hero band that lifts the photo off the dark canvas — big enough
//      to read at a glance. The earlier "small avatar inline with the
//      name" felt like a list-row item, not a profile.
//   2. Filmography split by type (movies first, then series) the way
//      Plex and Letterboxd surface a person — same titles, but the
//      grouping makes "what's this actor done" scannable.
//   3. Real posters via the wire's `poster_url`. The previous tile
//      was a deliberate placeholder under the assumption that adding
//      posters required N queries; the backend now JOINs the primary
//      image in a single query so the wire just carries one extra
//      column per row.
//   4. No bio yet — wiring TMDb's `/person/{id}` endpoint is real
//      work (provider extension + DB migration + scanner update) and
//      out of scope for this iteration.

function FilmographyTile({ entry }: { entry: FilmographyEntry }) {
  const { t } = useTranslation();
  const [imageFailed, setImageFailed] = useState(false);
  const showPoster = !!entry.poster_url && !imageFailed;
  return (
    <Link
      to={`/items/${entry.item_id}`}
      className="group flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-accent rounded-[--radius-lg]"
    >
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated ring-1 ring-border/40 transition-transform duration-300 group-hover:scale-[1.03] group-hover:ring-accent/40">
        {showPoster ? (
          <img
            src={entry.poster_url}
            alt={entry.title}
            loading="lazy"
            decoding="async"
            className="size-full object-cover"
            onError={() => setImageFailed(true)}
          />
        ) : (
          <div className="flex size-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-4xl font-bold text-text-muted">
              {entry.title.charAt(0).toUpperCase()}
            </span>
          </div>
        )}
        {/* Gradient overlay for legibility of the year chip on bright
            posters; only renders when we actually have a poster. */}
        {showPoster && (
          <div
            className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-black/70 to-transparent opacity-0 transition-opacity group-hover:opacity-100"
            aria-hidden
          />
        )}
      </div>
      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="line-clamp-2 text-sm font-medium text-text-primary group-hover:text-white transition-colors">
          {entry.title}
        </p>
        <div className="flex flex-wrap items-center gap-1.5 text-xs text-text-muted">
          {entry.year != null && <span>{entry.year}</span>}
          {entry.character && (
            <>
              {entry.year != null && <span className="text-text-muted/40">·</span>}
              <span className="truncate">
                {t("personDetail.asCharacter", { character: entry.character })}
              </span>
            </>
          )}
        </div>
      </div>
    </Link>
  );
}

function FilmographySection({
  title,
  entries,
}: {
  title: string;
  entries: FilmographyEntry[];
}) {
  if (entries.length === 0) return null;
  return (
    <section>
      <div className="mb-4 flex items-baseline gap-3">
        <h3 className="text-base font-semibold text-text-primary">{title}</h3>
        <span className="text-xs text-text-muted">{entries.length}</span>
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {entries.map((entry) => (
          <FilmographyTile key={entry.item_id} entry={entry} />
        ))}
      </div>
    </section>
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

  // Group + memoise filmography. The repo already returns a single
  // entry per item (deduped), sorted year-desc — we just split by
  // type so the grid renders movies and series as separate sections.
  const grouped = useMemo(() => {
    const movies: FilmographyEntry[] = [];
    const series: FilmographyEntry[] = [];
    for (const entry of person?.filmography ?? []) {
      if (entry.type === "series") series.push(entry);
      else movies.push(entry);
    }
    return { movies, series };
  }, [person?.filmography]);

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
  const totalCount = person.filmography.length;

  return (
    <div className="flex flex-col">
      {/* Hero band — soft gradient lift behind the avatar so the page
          stops looking like a list-item and starts looking like a
          profile. The blurred backdrop uses the photo itself when
          present, falling back to the surface gradient otherwise. */}
      <div className="relative overflow-hidden">
        {showPhoto && (
          <div
            aria-hidden
            className="absolute inset-0 -z-10 scale-110 bg-cover bg-center opacity-30 blur-3xl"
            style={{ backgroundImage: `url(${person.image_url})` }}
          />
        )}
        <div className="absolute inset-0 -z-10 bg-gradient-to-b from-bg-card/60 via-bg-card/85 to-bg-canvas" />

        <header className="flex flex-col items-center gap-6 px-6 py-10 text-center sm:flex-row sm:items-end sm:gap-8 sm:px-10 sm:py-12 sm:text-left">
          <div className="flex size-44 shrink-0 items-center justify-center overflow-hidden rounded-full bg-bg-elevated text-5xl font-bold text-text-muted ring-2 ring-border/60 shadow-xl shadow-black/40 sm:h-56 sm:w-56">
            {showPhoto ? (
              <img
                src={person.image_url}
                alt={person.name}
                loading="eager"
                decoding="async"
                width={224}
                height={224}
                className="size-full object-cover"
                onError={() => setPhotoFailed(true)}
              />
            ) : (
              person.name.charAt(0)
            )}
          </div>

          <div className="flex flex-col gap-2 sm:pb-2">
            {person.type && (
              <span className="text-xs font-semibold uppercase tracking-[0.2em] text-accent">
                {person.type}
              </span>
            )}
            <h1 className="text-3xl font-semibold text-text-primary drop-shadow-md sm:text-5xl">
              {person.name}
            </h1>
            {totalCount > 0 && (
              <p className="text-sm text-text-secondary">
                {t("personDetail.titlesCount", { count: totalCount })}
              </p>
            )}
          </div>
        </header>
      </div>

      <div className="flex flex-col gap-10 px-6 py-8 sm:px-10">
        {totalCount === 0 ? (
          <p className="text-sm text-text-muted">
            {t("personDetail.noFilmography")}
          </p>
        ) : (
          <>
            <div className="flex items-baseline gap-3">
              <h2 className="text-lg font-semibold text-text-primary">
                {t("personDetail.filmographyOnHubplay")}
              </h2>
              <span className="text-xs text-text-muted">
                {t("personDetail.filmography")}
              </span>
            </div>
            <FilmographySection
              title={t("personDetail.movies")}
              entries={grouped.movies}
            />
            <FilmographySection
              title={t("personDetail.series")}
              entries={grouped.series}
            />
          </>
        )}
      </div>
    </div>
  );
}
