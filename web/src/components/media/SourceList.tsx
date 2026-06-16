import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";
import { Play, X, RefreshCw, Download, Check } from "lucide-react";
import { api } from "@/api/client";
import { ApiError, type MediaSourceType, type TorrentSearchResult } from "@/api/types";
import { useMediaSources } from "@/hooks/useMediaSources";
import { useAuthStore } from "@/store/auth";
import { Button, Spinner } from "@/components/common";

interface SourceListProps {
  type: MediaSourceType;
  imdbId: string;
  /** Gate the query (e.g. only fetch when a panel is open). */
  enabled?: boolean;
  /** Optional play handler (parent owns playback). */
  onPlay?: (source: TorrentSearchResult) => void;
  /** Cabecera de la sección. La pinta el propio SourceList para poder
   *  ocultarla entera (no sólo el cuerpo) cuando no hay fuentes. */
  title?: string;
  /** Título/año reales de la obra: habilitan el pase de búsqueda por texto
   *  en el servidor (sin ellos muchos títulos salen "sin fuentes"). */
  mediaTitle?: string;
  year?: number;
}

// formatSize renders bytes as GB (decimal). Returns "" when unknown.
function formatSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return "";
  return `${(bytes / 1e9).toFixed(2)} GB`;
}

// langFlag maps a detected language tag to a flag/emoji (Torrentio-style).
const LANG_FLAG: Record<string, string> = {
  Spanish: "🇪🇸",
  Latino: "🌎",
  English: "🇬🇧",
  French: "🇫🇷",
  Dual: "🔀",
  Multi: "🌐",
  VOST: "💬",
};

function languageFlags(langs?: string[]): string {
  if (!langs) return "";
  return langs
    .filter((l) => l && l !== "Original")
    .map((l) => LANG_FLAG[l] ?? l)
    .join(" ");
}

// bestSrc is what we hand the player: prefer a magnet; fall back to the
// torrent_url (which may itself be a magnet the backend built).
function bestSrc(s: TorrentSearchResult): string {
  return s.magnet_uri || s.torrent_url;
}

// webRiskyCodec flags codecs that browsers usually can't decode in <video>
// (HEVC/x265, AV1, XviD) so the UI can warn before the user hits a black
// screen. H.264 in MP4 is the safe case.
function webRiskyCodec(codec?: string): boolean {
  return codec === "HEVC" || codec === "AV1" || codec === "XviD";
}

// groupByQuality buckets the (already sorted) sources by their quality
// label, preserving order — the Torrentio-style "4K / 1080p / 720p…"
// sections.
function groupByQuality(
  sources: TorrentSearchResult[],
  otherLabel: string,
): { label: string; items: TorrentSearchResult[] }[] {
  const order: string[] = [];
  const map = new Map<string, TorrentSearchResult[]>();
  for (const s of sources) {
    const label = s.quality || s.resolution || otherLabel;
    if (!map.has(label)) {
      map.set(label, []);
      order.push(label);
    }
    map.get(label)!.push(s);
  }
  return order.map((label) => ({ label, items: map.get(label)! }));
}

/**
 * SourceResults renders a grouped, Torrentio-style list of sources and
 * handles playback (delegated via `onPlay`, or a built-in player). It is
 * presentation-only — fetching lives in the caller — so both the per-item
 * SourceList and the general search page reuse it.
 */
export function SourceResults({
  sources,
  onPlay,
  downloadType,
}: {
  sources: TorrentSearchResult[];
  onPlay?: (source: TorrentSearchResult) => void;
  /** When set and the user is an admin, each row shows a "Download" button
   *  that saves the source to the library (this media type). */
  downloadType?: MediaSourceType;
}) {
  const { t } = useTranslation();
  const isAdmin = useAuthStore((s) => s.user?.role === "admin");
  const [playing, setPlaying] = useState<{ src: string; title: string } | null>(
    null,
  );
  const canDownload = isAdmin && downloadType !== undefined;
  const otherLabel = t("sources.other", { defaultValue: "Otros" });
  const groups = useMemo(
    () => groupByQuality(sources, otherLabel),
    [sources, otherLabel],
  );

  function handlePlay(s: TorrentSearchResult) {
    if (onPlay) {
      onPlay(s);
      return;
    }
    setPlaying({ src: api.torrentStreamURL(bestSrc(s)), title: s.title });
  }

  if (sources.length === 0) {
    return (
      <p className="py-4 text-sm text-text-muted">
        {t("sources.empty", {
          defaultValue: "No se encontraron fuentes para este título.",
        })}
      </p>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-4">
        {groups.map((g) => (
          <div key={g.label} className="flex flex-col gap-1.5">
            <h3 className="text-[11px] font-semibold uppercase tracking-widest text-text-muted">
              {g.label}
              <span className="ml-2 font-normal normal-case tracking-normal text-text-muted/70">
                {g.items.length}
              </span>
            </h3>
            <ul className="flex flex-col gap-1.5">
              {g.items.map((s) => (
                <SourceRow
                  key={s.infohash || s.identifier}
                  source={s}
                  onPlay={() => handlePlay(s)}
                  downloadType={canDownload ? downloadType : undefined}
                />
              ))}
            </ul>
          </div>
        ))}
      </div>

      {playing && (
        <SourcePlayerModal
          src={playing.src}
          title={playing.title}
          onClose={() => setPlaying(null)}
        />
      )}
    </>
  );
}

/**
 * SourceList resolves the sources for a title by IMDb id (via the Torznab
 * aggregator) and renders them with SourceResults.
 */
export function SourceList({ type, imdbId, enabled = true, onPlay, title, mediaTitle, year }: SourceListProps) {
  const { t } = useTranslation();
  const { sources, isLoading, isError, error, refetch } = useMediaSources(
    type,
    imdbId,
    { enabled, title: mediaTitle, year },
  );

  const featureDisabled =
    error instanceof ApiError &&
    (error.status === 404 ||
      (error.status === 503 && error.code === "INDEXER_DISABLED"));

  // Nada que ofrecer → no pintamos la sección (ni la cabecera "Fuentes"):
  // ni cuando no hay indexador configurado en el servidor, ni cuando el
  // título simplemente no tiene fuentes. Un encabezado vacío en cada ficha
  // es ruido. (En el modal de búsqueda sí se informa "sin resultados", pero
  // eso lo hace SourceResults, no este wrapper.)
  if (featureDisabled) return null;
  if (!isLoading && !isError && sources.length === 0) return null;

  const header = title ? (
    <h2 className="mb-3 text-lg font-semibold text-text-primary">{title}</h2>
  ) : null;

  if (isLoading) {
    return (
      <section>
        {header}
        <div className="flex items-center gap-2 py-6 text-sm text-text-muted">
          <Spinner size="sm" />
          {t("sources.loading", { defaultValue: "Buscando fuentes…" })}
        </div>
      </section>
    );
  }
  if (isError) {
    const rateLimited = error instanceof ApiError && error.code === "RATE_LIMITED";
    return (
      <section>
        {header}
        <div className="flex flex-col items-start gap-3 py-4">
          <p className="text-sm text-text-muted">
            {rateLimited
              ? t("sources.rateLimited", {
                  defaultValue:
                    "El indexador está saturando peticiones. Espera unos segundos y reintenta.",
                })
              : t("sources.error", { defaultValue: "No se pudieron obtener las fuentes." })}
          </p>
          <Button variant="secondary" size="sm" onClick={refetch}>
            <RefreshCw className="size-3.5" />
            {t("sources.retry", { defaultValue: "Reintentar" })}
          </Button>
        </div>
      </section>
    );
  }
  return (
    <section>
      {header}
      <SourceResults sources={sources} onPlay={onPlay} downloadType={type} />
    </section>
  );
}

function SourceRow({
  source,
  onPlay,
  downloadType,
}: {
  source: TorrentSearchResult;
  onPlay: () => void;
  downloadType?: MediaSourceType;
}) {
  const { t } = useTranslation();
  const size = formatSize(source.size_bytes);
  const flags = languageFlags(source.languages);
  return (
    <li className="flex items-center gap-3 rounded-lg border border-border bg-bg-base/40 px-3 py-2">
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13px] text-text-primary" title={source.title}>
          {source.title || source.identifier}
        </p>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-text-muted">
          <span title={t("sources.seedersLabel", { defaultValue: "Seeders" })}>
            👤 {source.seeders ?? 0}
          </span>
          {size ? <span>💾 {size}</span> : null}
          {source.provider ? <span>⚙️ {source.provider}</span> : null}
          {source.codec ? <span>{source.codec}</span> : null}
          {flags ? <span aria-hidden>{flags}</span> : null}
          {webRiskyCodec(source.codec) ? (
            <span
              className="text-warning"
              title={t("sources.webRisky", {
                defaultValue: "Este códec puede no reproducirse en el navegador",
              })}
            >
              ⚠
            </span>
          ) : null}
        </div>
      </div>
      {downloadType ? <DownloadButton source={source} type={downloadType} /> : null}
      <button
        type="button"
        onClick={onPlay}
        className="flex shrink-0 items-center gap-1.5 rounded-full bg-accent px-3 py-1.5 text-xs font-semibold text-white transition hover:opacity-90"
      >
        <Play className="size-3.5" fill="currentColor" />
        {t("sources.play", { defaultValue: "Reproducir" })}
      </button>
    </li>
  );
}

// DownloadButton starts a "download to library" for one source (admin).
function DownloadButton({
  source,
  type,
}: {
  source: TorrentSearchResult;
  type: MediaSourceType;
}) {
  const { t } = useTranslation();
  const m = useMutation({
    mutationFn: () => api.downloadSource(bestSrc(source), type),
  });
  return (
    <button
      type="button"
      onClick={() => m.mutate()}
      disabled={m.isPending || m.isSuccess}
      title={
        m.isError && m.error instanceof ApiError
          ? m.error.message
          : t("sources.downloadHint", { defaultValue: "Descargar a la biblioteca" })
      }
      className="flex shrink-0 items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-xs font-semibold text-text-secondary transition hover:border-border-strong hover:text-text-primary disabled:opacity-60"
    >
      {m.isSuccess ? <Check className="size-3.5" /> : <Download className="size-3.5" />}
      {m.isSuccess
        ? t("sources.queued", { defaultValue: "En cola" })
        : t("sources.download", { defaultValue: "Descargar" })}
    </button>
  );
}

function SourcePlayerModal({
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
          <span className="truncate text-sm font-semibold text-text-primary">{title}</span>
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
                {t("sources.playError", {
                  defaultValue:
                    "No se pudo reproducir. Puede que el streaming de torrent esté desactivado o que el formato no sea compatible con el navegador.",
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
