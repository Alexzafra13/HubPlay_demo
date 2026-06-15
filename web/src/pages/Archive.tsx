import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { m } from "framer-motion";
import { Film, Radio, Play, X, Search as SearchIcon } from "lucide-react";
import { api } from "@/api/client";
import {
  ApiError,
  type TorrentDiscoverResult,
  type TorrentSearchResult,
} from "@/api/types";
import { useAuthStore } from "@/store/auth";
import { Button, Input, Spinner, EmptyState } from "@/components/common";

// Archive — discover-first browse with in-browser playback, over LEGAL
// sources. Flow:
//   1. Search → TMDb metadata (posters/overview/year/id) via /torrent/discover.
//   2. Pick a title → resolve playable sources via /torrent/search (Internet
//      Archive + any provider the operator registered) by title+year.
//   3. Play in the browser.
// When no TMDb provider is configured, the page falls back to a plain
// catalogue text search (still legal sources). The engine + provider
// registry live in the Go backend (internal/torrentstream).
//
// Permission model (server-enforced): admins START a torrent (the one-time
// download bandwidth); any user PLAYS one that's already active.
export default function Archive() {
  const { t } = useTranslation();
  const isAdmin = useAuthStore((s) => s.user?.role === "admin");

  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [selected, setSelected] = useState<TorrentDiscoverResult | null>(null);
  const [playing, setPlaying] = useState<{ src: string; title: string } | null>(
    null,
  );

  const discover = useQuery({
    queryKey: ["torrent-discover", submitted],
    queryFn: () => api.discoverTorrents(submitted),
    enabled: submitted.length > 0,
    retry: false,
    staleTime: 5 * 60_000,
  });

  // Feature fully off (route not mounted / engine disabled).
  const featureDisabled =
    discover.error instanceof ApiError &&
    (discover.error.status === 404 ||
      (discover.error.status === 503 &&
        discover.error.code === "TORRENT_DISABLED"));
  // TMDb not configured → fall back to plain catalogue search.
  const noMetadata =
    discover.error instanceof ApiError &&
    discover.error.code === "NO_METADATA_PROVIDER";

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitted(term.trim());
  }

  const results = discover.data ?? [];

  return (
    <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-8 px-6 py-8 sm:px-10">
      <header className="flex flex-col gap-3">
        <h1
          className="text-[26px] font-semibold tracking-tight text-text-primary sm:text-[28px]"
          style={{ letterSpacing: "-0.015em" }}
        >
          {t("archive.title", { defaultValue: "Archive" })}
        </h1>
        <p className="max-w-2xl text-[13.5px] text-text-secondary">
          {t("archive.subtitle", {
            defaultValue:
              "Busca una película y reprodúcela desde fuentes de archivo legales (dominio público, Creative Commons, Internet Archive).",
          })}
        </p>

        <form onSubmit={onSubmit} className="flex max-w-xl items-end gap-2">
          <div className="flex-1">
            <Input
              label={t("archive.searchLabel", { defaultValue: "Buscar" })}
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              placeholder={t("archive.searchPlaceholder", {
                defaultValue: "Ej. Nosferatu, Chaplin, NASA…",
              })}
              icon={<SearchIcon className="size-4" strokeWidth={1.8} />}
              autoFocus
            />
          </div>
          <Button type="submit" size="lg" disabled={!term.trim()}>
            {t("archive.searchCta", { defaultValue: "Buscar" })}
          </Button>
        </form>

        {!isAdmin && (
          <p className="text-xs text-text-muted">
            {t("archive.userHint", {
              defaultValue:
                "Puedes reproducir lo que un administrador haya añadido. Si algo no está activo, pídeselo a un admin.",
            })}
          </p>
        )}
      </header>

      {featureDisabled ? (
        <EmptyState
          title={t("archive.disabledTitle", { defaultValue: "Función no activada" })}
          description={t("archive.disabledDesc", {
            defaultValue:
              "El streaming de torrent está desactivado en este servidor (torrent.enabled).",
          })}
          icon={<Radio strokeWidth={1.5} />}
        />
      ) : noMetadata ? (
        // No TMDb → degrade to plain catalogue text search.
        <CatalogFallback
          query={submitted}
          onPlay={(src, title) => setPlaying({ src, title })}
        />
      ) : !submitted ? (
        <EmptyState
          title={t("archive.idleTitle", { defaultValue: "Empieza a buscar" })}
          description={t("archive.idleDesc", {
            defaultValue: "Escribe el título de una película.",
          })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : discover.isFetching ? (
        <div className="flex items-center justify-center py-24">
          <Spinner size="md" />
        </div>
      ) : discover.error ? (
        <EmptyState
          title={t("archive.errorTitle", { defaultValue: "No se pudo buscar" })}
          description={
            discover.error instanceof Error
              ? discover.error.message
              : String(discover.error)
          }
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : results.length === 0 ? (
        <EmptyState
          title={t("archive.noResultsTitle", { defaultValue: "Sin resultados" })}
          description={t("archive.noResultsDesc", {
            defaultValue: "Prueba con otro título.",
          })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
          {results.map((r, i) => (
            <MovieCard
              key={r.tmdb_id || `${r.title}-${i}`}
              movie={r}
              index={i}
              onSelect={() => setSelected(r)}
            />
          ))}
        </ul>
      )}

      {selected && (
        <MovieDetailModal
          movie={selected}
          onClose={() => setSelected(null)}
          onPlay={(src) => {
            setSelected(null);
            setPlaying({ src, title: selected.title });
          }}
        />
      )}

      {playing && (
        <PlayerModal
          src={playing.src}
          title={playing.title}
          onClose={() => setPlaying(null)}
        />
      )}
    </div>
  );
}

// ─── Movie card (TMDb poster, CSP allows image.tmdb.org) ───────────────

function MovieCard({
  movie,
  index,
  onSelect,
}: {
  movie: TorrentDiscoverResult;
  index: number;
  onSelect: () => void;
}) {
  const { t } = useTranslation();
  return (
    <m.li
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, delay: Math.min(index * 0.02, 0.3) }}
      className="group flex flex-col gap-2"
    >
      <button
        type="button"
        onClick={onSelect}
        className="relative aspect-[2/3] w-full overflow-hidden rounded-xl border border-border bg-bg-card text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent group-hover:-translate-y-0.5 group-hover:border-border-strong group-hover:shadow-xl"
        aria-label={t("archive.detailAria", {
          defaultValue: "Ver fuentes de {{title}}",
          title: movie.title,
        })}
      >
        {movie.poster_url ? (
          <img
            src={movie.poster_url}
            alt=""
            loading="lazy"
            className="size-full object-cover"
          />
        ) : (
          <span className="flex size-full items-center justify-center bg-gradient-to-br from-bg-hover to-bg-card">
            <Film className="size-10 text-text-muted" strokeWidth={1.2} />
          </span>
        )}
        <span className="absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition group-hover:bg-black/30 group-hover:opacity-100">
          <span className="flex size-12 items-center justify-center rounded-full bg-black/50 ring-1 ring-white/30 backdrop-blur group-hover:bg-accent/85">
            <Play className="size-5 text-white" fill="currentColor" />
          </span>
        </span>
      </button>
      <div className="flex min-w-0 flex-col px-0.5">
        <span className="truncate text-sm font-medium text-text-primary" title={movie.title}>
          {movie.title}
        </span>
        {movie.year ? (
          <span className="text-xs text-text-muted">{movie.year}</span>
        ) : null}
      </div>
    </m.li>
  );
}

// ─── Detail modal: metadata + legal sources resolved by title+year ─────

function MovieDetailModal({
  movie,
  onClose,
  onPlay,
}: {
  movie: TorrentDiscoverResult;
  onClose: () => void;
  onPlay: (src: string) => void;
}) {
  const { t } = useTranslation();
  const q = [movie.title, movie.year].filter(Boolean).join(" ");
  const sources = useQuery({
    queryKey: ["torrent-sources", q],
    queryFn: () => api.searchTorrents(q, 20),
    retry: false,
    staleTime: 5 * 60_000,
  });
  const list = sources.data ?? [];

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
            <img
              src={movie.poster_url}
              alt=""
              className="h-60 w-40 flex-shrink-0 self-center rounded-lg object-cover sm:self-start"
            />
          ) : null}
          <div className="min-w-0 flex-1">
            <h2 className="text-xl font-semibold text-text-primary">
              {movie.title}
              {movie.year ? (
                <span className="ml-2 text-base font-normal text-text-muted">
                  {movie.year}
                </span>
              ) : null}
            </h2>
            {movie.overview ? (
              <p className="mt-2 line-clamp-5 text-[13px] text-text-secondary">
                {movie.overview}
              </p>
            ) : null}

            <div className="mt-4">
              <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-widest text-text-muted">
                {t("archive.sources", { defaultValue: "Fuentes legales" })}
              </h3>
              {sources.isFetching ? (
                <div className="flex items-center gap-2 py-4 text-sm text-text-muted">
                  <Spinner size="sm" />
                  {t("archive.sourcesLoading", { defaultValue: "Buscando fuentes…" })}
                </div>
              ) : list.length === 0 ? (
                <p className="py-3 text-sm text-text-muted">
                  {t("archive.noSources", {
                    defaultValue:
                      "No se encontraron fuentes legales para este título.",
                  })}
                </p>
              ) : (
                <ul className="flex flex-col gap-1.5">
                  {list.map((s) => (
                    <SourceRow
                      key={s.identifier}
                      source={s}
                      onPlay={() => onPlay(api.torrentStreamURL(s.torrent_url))}
                    />
                  ))}
                </ul>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function SourceRow({
  source,
  onPlay,
}: {
  source: TorrentSearchResult;
  onPlay: () => void;
}) {
  const { t } = useTranslation();
  return (
    <li className="flex items-center gap-2 rounded-lg border border-border bg-bg-base/40 px-3 py-2">
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13px] text-text-primary" title={source.title}>
          {source.title || source.identifier}
        </p>
        <p className="truncate text-[11px] text-text-muted">
          {[source.provider, source.mediatype, source.year]
            .filter(Boolean)
            .join(" · ")}
        </p>
      </div>
      <button
        type="button"
        onClick={onPlay}
        className="flex shrink-0 items-center gap-1.5 rounded-full bg-accent px-3 py-1.5 text-xs font-semibold text-white transition hover:opacity-90"
      >
        <Play className="size-3.5" fill="currentColor" />
        {t("archive.play", { defaultValue: "Reproducir" })}
      </button>
    </li>
  );
}

// ─── Plain catalogue fallback (no TMDb provider configured) ────────────

function CatalogFallback({
  query,
  onPlay,
}: {
  query: string;
  onPlay: (src: string, title: string) => void;
}) {
  const { t } = useTranslation();
  const { data, isFetching, error } = useQuery({
    queryKey: ["torrent-search", query],
    queryFn: () => api.searchTorrents(query, 36),
    enabled: query.length > 0,
    retry: false,
    staleTime: 5 * 60_000,
  });
  const list = data ?? [];

  if (!query) {
    return (
      <EmptyState
        title={t("archive.idleTitle", { defaultValue: "Empieza a buscar" })}
        description={t("archive.idleDescCatalog", {
          defaultValue: "Busca en el catálogo de archivo.",
        })}
        icon={<SearchIcon strokeWidth={1.5} />}
      />
    );
  }
  if (isFetching) {
    return (
      <div className="flex items-center justify-center py-24">
        <Spinner size="md" />
      </div>
    );
  }
  if (error) {
    return (
      <EmptyState
        title={t("archive.errorTitle", { defaultValue: "No se pudo buscar" })}
        description={error instanceof Error ? error.message : String(error)}
        icon={<SearchIcon strokeWidth={1.5} />}
      />
    );
  }
  if (list.length === 0) {
    return (
      <EmptyState
        title={t("archive.noResultsTitle", { defaultValue: "Sin resultados" })}
        description={t("archive.noResultsDesc", { defaultValue: "Prueba con otro título." })}
        icon={<SearchIcon strokeWidth={1.5} />}
      />
    );
  }
  return (
    <ul className="flex flex-col gap-1.5">
      {list.map((s) => (
        <SourceRow
          key={s.identifier}
          source={s}
          onPlay={() =>
            onPlay(api.torrentStreamURL(s.torrent_url), s.title || s.identifier)
          }
        />
      ))}
    </ul>
  );
}

// ─── Player overlay ────────────────────────────────────────────────────

function PlayerModal({
  src,
  title,
  onClose,
}: {
  src: string;
  title: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [errored, setErrored] = useState(false);

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/85 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-4xl overflow-hidden rounded-2xl border border-border bg-bg-card shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <span className="truncate text-sm font-semibold text-text-primary">
            {title}
          </span>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("common.close", { defaultValue: "Cerrar" })}
            className="flex size-8 shrink-0 items-center justify-center rounded-full text-text-muted transition-colors hover:bg-bg-hover hover:text-text-primary"
          >
            <X className="size-4" />
          </button>
        </div>
        <div className="relative aspect-video w-full bg-black">
          {errored ? (
            <div className="flex h-full items-center justify-center px-6 text-center">
              <p className="text-sm text-text-secondary">
                {t("archive.playError", {
                  defaultValue:
                    "No se pudo reproducir. Puede que aún no esté activo (pide a un admin que lo añada) o que el formato no sea compatible con el navegador.",
                })}
              </p>
            </div>
          ) : (
            <video
              key={src}
              src={src}
              controls
              autoPlay
              className="h-full w-full"
              onError={() => setErrored(true)}
            />
          )}
        </div>
      </div>
    </div>
  );
}
