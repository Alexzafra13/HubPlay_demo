import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Search as SearchIcon, Film, X } from "lucide-react";
import { api } from "@/api/client";
import {
  ApiError,
  type MediaSourceType,
  type TorrentDiscoverResult,
} from "@/api/types";
import { useDiscoverSources, useMediaSearch } from "@/hooks/useMediaSources";
import { SourceResults } from "@/components/media/SourceList";
import { Button, Input, Spinner, EmptyState } from "@/components/common";

// Archive — discovery-first search backed by the operator's indexers
// (Prowlarr). Type a title → TMDb poster grid → pick → sources resolved by
// IMDb id (Torrentio-style, reliable). When no TMDb provider is configured
// it degrades to a plain free-text torrent search.
export default function Archive() {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [type, setType] = useState<MediaSourceType>("movie");
  const [selected, setSelected] = useState<TorrentDiscoverResult | null>(null);

  const discover = useQuery({
    queryKey: ["torrent-discover", type, submitted],
    queryFn: () => api.discoverTitles(submitted, type),
    enabled: submitted.length > 0,
    retry: false,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
  });

  // No TMDb provider (route not mounted → 404, or explicit 503) → fall back
  // to plain free-text search.
  const noTmdb =
    discover.error instanceof ApiError &&
    (discover.error.status === 404 || discover.error.code === "NO_METADATA_PROVIDER");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSelected(null);
    setSubmitted(term.trim());
  }

  const results = discover.data ?? [];

  return (
    <div className="mx-auto flex w-full max-w-[1200px] flex-col gap-8 px-6 py-8 sm:px-10">
      <header className="flex flex-col gap-3">
        <h1 className="text-[26px] font-semibold tracking-tight text-text-primary sm:text-[28px]">
          {t("archive.title", { defaultValue: "Buscar" })}
        </h1>
        <p className="max-w-2xl text-[13.5px] text-text-secondary">
          {t("archive.subtitle", {
            defaultValue:
              "Busca un título, elige y resuelve fuentes desde tus indexadores (Prowlarr).",
          })}
        </p>

        <form onSubmit={onSubmit} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div className="inline-flex shrink-0 overflow-hidden rounded-lg border border-border">
            {(["movie", "series"] as MediaSourceType[]).map((tt) => (
              <button
                key={tt}
                type="button"
                onClick={() => setType(tt)}
                className={[
                  "px-3 py-2 text-sm font-medium transition-colors",
                  type === tt
                    ? "bg-accent text-white"
                    : "bg-bg-card text-text-secondary hover:text-text-primary",
                ].join(" ")}
              >
                {tt === "movie"
                  ? t("archive.movie", { defaultValue: "Películas" })
                  : t("archive.series", { defaultValue: "Series" })}
              </button>
            ))}
          </div>
          <div className="flex-1">
            <Input
              label={t("archive.searchLabel", { defaultValue: "Buscar" })}
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              placeholder={t("archive.searchPlaceholder", {
                defaultValue: "Ej. The Matrix, Breaking Bad…",
              })}
              icon={<SearchIcon className="size-4" strokeWidth={1.8} />}
              autoFocus
            />
          </div>
          <Button type="submit" size="lg" disabled={!term.trim()}>
            {t("archive.searchCta", { defaultValue: "Buscar" })}
          </Button>
        </form>
      </header>

      {!submitted ? (
        <EmptyState
          title={t("archive.idleTitle", { defaultValue: "Empieza a buscar" })}
          description={t("archive.idleDesc", { defaultValue: "Escribe un título y pulsa Buscar." })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : noTmdb ? (
        // Fallback: plain free-text torrent search (no posters).
        <TextSearch type={type} query={submitted} />
      ) : discover.isFetching ? (
        <div className="flex items-center justify-center py-24">
          <Spinner size="md" />
        </div>
      ) : discover.error ? (
        <EmptyState
          title={t("archive.errorTitle", { defaultValue: "No se pudo buscar" })}
          description={discover.error instanceof Error ? discover.error.message : String(discover.error)}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : results.length === 0 ? (
        <EmptyState
          title={t("archive.noResultsTitle", { defaultValue: "Sin resultados" })}
          description={t("archive.noResultsDesc", { defaultValue: "Prueba con otro título." })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
          {results.map((r, i) => (
            <MovieCard key={r.tmdb_id || `${r.title}-${i}`} movie={r} onSelect={() => setSelected(r)} />
          ))}
        </ul>
      )}

      {selected && (
        <SourcesModal type={type} movie={selected} onClose={() => setSelected(null)} />
      )}
    </div>
  );
}

function MovieCard({ movie, onSelect }: { movie: TorrentDiscoverResult; onSelect: () => void }) {
  return (
    <li className="group flex flex-col gap-2">
      <button
        type="button"
        onClick={onSelect}
        className="relative aspect-[2/3] w-full overflow-hidden rounded-xl border border-border bg-bg-card text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent group-hover:-translate-y-0.5 group-hover:border-border-strong"
      >
        {movie.poster_url ? (
          <img src={movie.poster_url} alt="" loading="lazy" className="size-full object-cover" />
        ) : (
          <span className="flex size-full items-center justify-center">
            <Film className="size-10 text-text-muted" strokeWidth={1.2} />
          </span>
        )}
      </button>
      <div className="flex min-w-0 flex-col px-0.5">
        <span className="truncate text-sm font-medium text-text-primary" title={movie.title}>
          {movie.title}
        </span>
        {movie.year ? <span className="text-xs text-text-muted">{movie.year}</span> : null}
      </div>
    </li>
  );
}

function SourcesModal({
  type,
  movie,
  onClose,
}: {
  type: MediaSourceType;
  movie: TorrentDiscoverResult;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { sources, isLoading, isError } = useDiscoverSources(type, movie.tmdb_id);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-label={movie.title}
      onClick={onClose}
    >
      <div
        className="relative max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-2xl border border-border bg-bg-card shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          type="button"
          onClick={onClose}
          aria-label={t("common.close", { defaultValue: "Cerrar" })}
          className="absolute right-3 top-3 z-10 flex size-8 items-center justify-center rounded-full bg-black/40 text-white/80 backdrop-blur transition-colors hover:bg-black/60 hover:text-white"
        >
          <X className="size-4" />
        </button>
        <div className="flex flex-col gap-4 p-5 sm:flex-row">
          {movie.poster_url ? (
            <img src={movie.poster_url} alt="" className="h-60 w-40 flex-shrink-0 self-center rounded-lg object-cover sm:self-start" />
          ) : null}
          <div className="min-w-0 flex-1">
            <h2 className="text-xl font-semibold text-text-primary">
              {movie.title}
              {movie.year ? <span className="ml-2 text-base font-normal text-text-muted">{movie.year}</span> : null}
            </h2>
            {movie.overview ? <p className="mt-2 line-clamp-5 text-[13px] text-text-secondary">{movie.overview}</p> : null}
            <div className="mt-4">
              <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-widest text-text-muted">
                {t("sources.title", { defaultValue: "Fuentes" })}
              </h3>
              {isLoading ? (
                <div className="flex items-center gap-2 py-4 text-sm text-text-muted">
                  <Spinner size="sm" />
                  {t("sources.loading", { defaultValue: "Buscando fuentes…" })}
                </div>
              ) : isError ? (
                <p className="py-3 text-sm text-text-muted">
                  {t("sources.error", { defaultValue: "No se pudieron obtener las fuentes." })}
                </p>
              ) : (
                <SourceResults sources={sources} downloadType={type} />
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// TextSearch is the no-TMDb fallback: plain free-text torrent search.
function TextSearch({ type, query }: { type: MediaSourceType; query: string }) {
  const { t } = useTranslation();
  const { sources, isLoading, isError } = useMediaSearch(type, query);
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-24">
        <Spinner size="md" />
      </div>
    );
  }
  if (isError) {
    return (
      <EmptyState
        title={t("archive.errorTitle", { defaultValue: "No se pudo buscar" })}
        description={t("archive.noResultsDesc", { defaultValue: "Prueba con otro título." })}
        icon={<SearchIcon strokeWidth={1.5} />}
      />
    );
  }
  return <SourceResults sources={sources} downloadType={type} />;
}
