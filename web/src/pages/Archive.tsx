import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Search as SearchIcon } from "lucide-react";
import { ApiError, type MediaSourceType } from "@/api/types";
import { useMediaSearch } from "@/hooks/useMediaSources";
import { SourceResults } from "@/components/media/SourceList";
import { Button, Input, Spinner, EmptyState } from "@/components/common";

// Archive — general torrent search backed by the operator's own indexers
// (Prowlarr/Jackett via the Torznab aggregator). Type a title, pick
// movie/series, get sources grouped by quality and play them through
// HubPlay. The indexers are managed in Admin → System → Indexers.
export default function Archive() {
  const { t } = useTranslation();

  const [term, setTerm] = useState("");
  const [type, setType] = useState<MediaSourceType>("movie");
  const [submitted, setSubmitted] = useState("");

  const { sources, isLoading, isError, error } = useMediaSearch(type, submitted);

  // Feature off on this server (no indexer configured / route not mounted).
  const featureDisabled =
    error instanceof ApiError &&
    (error.status === 404 ||
      (error.status === 503 && error.code === "INDEXER_DISABLED"));

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitted(term.trim());
  }

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-col gap-8 px-6 py-8 sm:px-10">
      <header className="flex flex-col gap-3">
        <h1 className="text-[26px] font-semibold tracking-tight text-text-primary sm:text-[28px]">
          {t("archive.title", { defaultValue: "Buscar" })}
        </h1>
        <p className="max-w-2xl text-[13.5px] text-text-secondary">
          {t("archive.subtitle", {
            defaultValue:
              "Busca por título en tus indexadores (Prowlarr/Jackett) y reproduce vía HubPlay.",
          })}
        </p>

        <form onSubmit={onSubmit} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          {/* Movie / Series toggle */}
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

      {featureDisabled ? (
        <EmptyState
          title={t("archive.disabledTitle", { defaultValue: "Sin indexadores" })}
          description={t("archive.disabledDesc", {
            defaultValue:
              "No hay ningún indexador configurado. Añádelo en Admin → Sistema → Indexadores.",
          })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : !submitted ? (
        <EmptyState
          title={t("archive.idleTitle", { defaultValue: "Empieza a buscar" })}
          description={t("archive.idleDesc", {
            defaultValue: "Escribe un título y pulsa Buscar.",
          })}
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : isLoading ? (
        <div className="flex items-center justify-center py-24">
          <Spinner size="md" />
        </div>
      ) : isError ? (
        <EmptyState
          title={t("archive.errorTitle", { defaultValue: "No se pudo buscar" })}
          description={
            error instanceof ApiError && error.code === "RATE_LIMITED"
              ? t("archive.rateLimited", {
                  defaultValue:
                    "El indexador está saturando peticiones. Espera unos segundos y reintenta.",
                })
              : error instanceof Error
                ? error.message
                : String(error)
          }
          icon={<SearchIcon strokeWidth={1.5} />}
        />
      ) : (
        <SourceResults sources={sources} downloadType={type} />
      )}
    </div>
  );
}
