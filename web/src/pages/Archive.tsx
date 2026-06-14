import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { m } from "framer-motion";
import { Film, Music, Radio, Play, X, Search as SearchIcon } from "lucide-react";
import { api } from "@/api/client";
import { ApiError, type TorrentSearchResult } from "@/api/types";
import { useAuthStore } from "@/store/auth";
import { Button, Input, Spinner, EmptyState } from "@/components/common";

// Archive — free-text search over the server's LEGAL catalogue (Internet
// Archive) with in-browser playback. The engine + provider registry live
// in the Go backend (internal/torrentstream); this page is a thin client
// over /torrent/search + /torrent/stream.
//
// Permission model (enforced server-side): admins START a torrent
// (spending the one-time download bandwidth); any user can PLAY one that's
// already active. A non-admin who hits "play" on inactive content gets a
// 403 → the player surfaces a "ask an admin to add it" message.
export default function Archive() {
  const { t } = useTranslation();
  const isAdmin = useAuthStore((s) => s.user?.role === "admin");

  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [playing, setPlaying] = useState<TorrentSearchResult | null>(null);

  const { data, isFetching, error } = useQuery({
    queryKey: ["torrent-search", submitted],
    queryFn: () => api.searchTorrents(submitted, 36),
    enabled: submitted.length > 0,
    retry: false,
    staleTime: 5 * 60_000,
  });
  const results = data ?? [];

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitted(term.trim());
  }

  // A 404/503 from the search endpoint means the feature is off on this
  // server (torrent.enabled=false / route not mounted).
  const featureDisabled =
    error instanceof ApiError && (error.status === 404 || error.status === 503);

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
              "Busca y reproduce contenido de archivo (Internet Archive): dominio público, Creative Commons y material libre.",
          })}
        </p>

        <form onSubmit={onSubmit} className="flex max-w-xl items-end gap-2">
          <div className="flex-1">
            <Input
              label={t("archive.searchLabel", { defaultValue: "Buscar" })}
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              placeholder={t("archive.searchPlaceholder", {
                defaultValue: "Ej. Charlie Chaplin, NASA, jazz…",
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

      {/* States */}
      {featureDisabled ? (
        <EmptyState
          title={t("archive.disabledTitle", { defaultValue: "Función no activada" })}
          description={t("archive.disabledDesc", {
            defaultValue:
              "El streaming de torrent está desactivado en este servidor (torrent.enabled).",
          })}
          icon={<Radio strokeWidth={1.5} />}
        />
      ) : error ? (
        <EmptyState
          title={t("archive.errorTitle", { defaultValue: "No se pudo buscar" })}
          description={
            error instanceof Error ? error.message : String(error)
          }
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : !submitted ? (
        <EmptyState
          title={t("archive.idleTitle", { defaultValue: "Empieza a buscar" })}
          description={t("archive.idleDesc", {
            defaultValue: "Escribe arriba para buscar en el catálogo.",
          })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : isFetching ? (
        <div className="flex items-center justify-center py-24">
          <Spinner size="md" />
        </div>
      ) : results.length === 0 ? (
        <EmptyState
          title={t("archive.noResultsTitle", { defaultValue: "Sin resultados" })}
          description={t("archive.noResultsDesc", {
            defaultValue: "Prueba con otros términos.",
          })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {results.map((r, i) => (
            <ResultCard
              key={r.identifier}
              result={r}
              index={i}
              onPlay={() => setPlaying(r)}
            />
          ))}
        </ul>
      )}

      {playing && (
        <PlayerModal result={playing} onClose={() => setPlaying(null)} />
      )}
    </div>
  );
}

// MediaGlyph renders the placeholder icon for a mediatype (we don't load
// external poster images — CSP keeps img-src locked to self).
function MediaGlyph({ mediatype, className }: { mediatype: string; className: string }) {
  if (mediatype === "audio" || mediatype === "etree") {
    return <Music className={className} strokeWidth={1.2} />;
  }
  return <Film className={className} strokeWidth={1.2} />;
}

function ResultCard({
  result,
  index,
  onPlay,
}: {
  result: TorrentSearchResult;
  index: number;
  onPlay: () => void;
}) {
  const { t } = useTranslation();
  // Deterministic hue from the identifier so each card keeps a stable
  // tint without any external image.
  const hue = hashHue(result.identifier);
  const bg = `linear-gradient(150deg, hsl(${hue} 45% 22%) 0%, hsl(${(hue + 40) % 360} 40% 12%) 100%)`;

  return (
    <m.li
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, delay: Math.min(index * 0.02, 0.3) }}
      className="group flex flex-col gap-2"
    >
      <button
        type="button"
        onClick={onPlay}
        className="relative aspect-video w-full overflow-hidden rounded-xl border border-border text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent group-hover:-translate-y-0.5 group-hover:border-border-strong group-hover:shadow-xl"
        style={{ background: bg }}
        aria-label={t("archive.playAria", {
          defaultValue: "Reproducir {{title}}",
          title: result.title,
        })}
      >
        <MediaGlyph
          mediatype={result.mediatype}
          className="absolute left-1/2 top-1/2 size-10 -translate-x-1/2 -translate-y-1/2 text-white/20"
        />
        <span className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity group-hover:opacity-100">
          <span className="flex size-12 items-center justify-center rounded-full bg-black/45 ring-1 ring-white/30 backdrop-blur transition group-hover:bg-accent/85">
            <Play className="size-5 text-white" fill="currentColor" />
          </span>
        </span>
        {result.provider && (
          <span className="absolute left-2 top-2 rounded bg-black/55 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider text-white/85 backdrop-blur">
            {result.provider}
          </span>
        )}
      </button>
      <div className="flex min-w-0 flex-col px-0.5">
        <span className="truncate text-sm font-medium text-text-primary" title={result.title}>
          {result.title || result.identifier}
        </span>
        <span className="truncate text-xs text-text-muted">
          {[result.mediatype, result.year].filter(Boolean).join(" · ")}
        </span>
      </div>
    </m.li>
  );
}

function PlayerModal({
  result,
  onClose,
}: {
  result: TorrentSearchResult;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [errored, setErrored] = useState(false);
  const src = api.torrentStreamURL(result.torrent_url);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-label={result.title}
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-4xl overflow-hidden rounded-2xl border border-border bg-bg-card shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <span className="truncate text-sm font-semibold text-text-primary">
            {result.title || result.identifier}
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
            <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
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

// hashHue maps a string to a stable hue (0-359) for the placeholder tint.
function hashHue(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) >>> 0;
  }
  return h % 360;
}
