import { useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useStudio } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";
import type { StudioItem } from "@/api/types";

// /studios/:slug — collection page for a single production company /
// network. Reached by clicking the studio mark on a movie or series
// detail page. The page mirrors PersonDetail's structure (hero band
// + grouped grid) so the look stays coherent across "browse by X"
// surfaces; the difference is the hero gets the studio logo on a
// white pill (TMDb logos arrive with arbitrary foreground colours,
// the same trick the detail page uses) instead of an avatar circle.

function StudioTile({ item }: { item: StudioItem }) {
  const [imageFailed, setImageFailed] = useState(false);
  const showPoster = !!item.poster_url && !imageFailed;
  const href = item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;
  return (
    <Link
      to={href}
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

function GridSection({ title, items }: { title: string; items: StudioItem[] }) {
  if (items.length === 0) return null;
  return (
    <section>
      <div className="mb-4 flex items-baseline gap-3">
        <h3 className="text-base font-semibold text-text-primary">{title}</h3>
        <span className="text-xs text-text-muted">{items.length}</span>
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((item) => (
          <StudioTile key={item.id} item={item} />
        ))}
      </div>
    </section>
  );
}

export default function StudioDetail() {
  const { t } = useTranslation();
  const { slug } = useParams<{ slug: string }>();
  const { data: studio, isLoading, isError } = useStudio(slug ?? "");

  const grouped = useMemo(() => {
    const movies: StudioItem[] = [];
    const series: StudioItem[] = [];
    for (const item of studio?.items ?? []) {
      if (item.type === "series") series.push(item);
      else movies.push(item);
    }
    return { movies, series };
  }, [studio?.items]);

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (isError || !studio) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <EmptyState
          title={t("studioDetail.notFoundTitle")}
          description={t("studioDetail.notFoundDescription")}
        />
      </div>
    );
  }

  const totalCount = studio.items.length;

  return (
    <div className="flex flex-col">
      {/* Hero band — same gradient lift PersonDetail uses, but the
          centre slot is the white-pilled studio logo instead of a
          circular avatar. The pill renders ~3× the detail-page
          dimensions so the brand mark reads as the page subject. */}
      <div className="relative overflow-hidden">
        <div className="absolute inset-0 -z-10 bg-gradient-to-b from-bg-card/60 via-bg-card/85 to-bg-canvas" />
        <header className="flex flex-col items-center gap-6 px-6 py-10 text-center sm:flex-row sm:items-end sm:gap-8 sm:px-10 sm:py-12 sm:text-left">
          <div className="flex h-32 w-72 shrink-0 items-center justify-center rounded-xl bg-white/95 px-5 shadow-xl shadow-black/40 sm:h-36 sm:w-80">
            {studio.logo_url ? (
              <img
                src={studio.logo_url}
                alt={studio.name}
                loading="eager"
                decoding="async"
                className="max-h-20 max-w-full object-contain"
              />
            ) : (
              <span className="text-2xl font-bold text-bg-canvas">
                {studio.name}
              </span>
            )}
          </div>

          <div className="flex flex-col gap-2 sm:pb-2">
            <h1 className="text-3xl font-semibold text-text-primary drop-shadow-md sm:text-5xl">
              {studio.name}
            </h1>
            {totalCount > 0 && (
              <p className="text-sm text-text-secondary">
                {t("studioDetail.titlesCount", { count: totalCount })}
              </p>
            )}
          </div>
        </header>
      </div>

      <div className="flex flex-col gap-10 px-6 py-8 sm:px-10">
        {totalCount === 0 ? (
          <p className="text-sm text-text-muted">{t("studioDetail.noItems")}</p>
        ) : (
          <>
            <GridSection title={t("studioDetail.movies")} items={grouped.movies} />
            <GridSection title={t("studioDetail.series")} items={grouped.series} />
          </>
        )}
      </div>
    </div>
  );
}
