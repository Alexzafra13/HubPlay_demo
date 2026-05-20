// IdentifyDialog — modal admin para reidentificar manualmente un item
// contra TMDb. Patrón Plex/Jellyfin:
//
//   1. Se abre con el título y año actuales como semilla de búsqueda.
//   2. El operador puede afinar el título y el año, o pegar un id TMDb
//      directo (futuro — hoy sólo búsqueda por título).
//   3. La lista de candidatos sale como tarjetas con póster + título +
//      año + sinopsis corta. La decisión visual evita que se elija el
//      match equivocado por confusión de títulos parecidos.
//   4. Click en una tarjeta + "Aplicar" → POST /identify, la página
//      detalle se refresca con los nuevos datos.
//
// El backend se encarga de borrar imágenes/metadata antiguas antes de
// aplicar el nuevo match, así el operador no tiene que orquestar pasos
// intermedios — un sólo gesto reescribe todo el item.

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Search, Check, AlertCircle } from "lucide-react";

import { useApplyIdentify, useIdentifyCandidates } from "@/api/hooks";
import type { IdentifyCandidate, ItemDetail } from "@/api/types";
import { Modal } from "@/components/common/Modal";
import { Button, Spinner } from "@/components/common";

interface IdentifyDialogProps {
  isOpen: boolean;
  onClose: () => void;
  item: ItemDetail;
}

export function IdentifyDialog({ isOpen, onClose, item }: IdentifyDialogProps) {
  const { t } = useTranslation();

  // Estado del formulario. Semilla = título + año actuales del item.
  // Re-semilla al abrir y cada vez que el operador cierre y vuelva a
  // abrir el modal — no en cada render del item, para que un refetch
  // de fondo no pise lo que está escribiendo.
  const [query, setQuery] = useState(item.title);
  const [year, setYear] = useState<string>(item.year ? String(item.year) : "");
  const [hasSearched, setHasSearched] = useState(false);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [seededOpen, setSeededOpen] = useState(false);

  // State-update-during-render: re-siembra los inputs al pasar de
  // cerrado a abierto. Sin esto los inputs se quedan con el último
  // estado que el operador dejó la vez anterior, que rara vez es el
  // título correcto del item actual. Mismo patrón que AdminChannelOrderPanel.
  if (isOpen && !seededOpen) {
    setSeededOpen(true);
    setQuery(item.title);
    setYear(item.year ? String(item.year) : "");
    setHasSearched(false);
    setSelectedID(null);
  } else if (!isOpen && seededOpen) {
    setSeededOpen(false);
  }

  const yearNum = year ? parseInt(year, 10) : undefined;
  const candidatesQ = useIdentifyCandidates(item.id, {
    query: query.trim() || undefined,
    year: yearNum && !Number.isNaN(yearNum) ? yearNum : undefined,
    enabled: hasSearched,
  });
  const apply = useApplyIdentify(item.id);

  const handleSearch = useCallback(() => {
    setSelectedID(null);
    setHasSearched(true);
    // Si ya hay datos cacheados de una búsqueda anterior con los mismos
    // params, refetch para asegurar que el operador ve TMDb actual.
    candidatesQ.refetch();
  }, [candidatesQ]);

  const handleApply = useCallback(async () => {
    if (!selectedID) return;
    await apply.mutateAsync({ provider: "tmdb", external_id: selectedID });
    onClose();
  }, [apply, onClose, selectedID]);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("identify.title", { defaultValue: "Identify item" })}
      size="lg"
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-text-muted">
          {t("identify.subtitle", {
            defaultValue:
              "Si el match es incorrecto, busca el título correcto y aplícalo. Esto sobrescribe título, sinopsis, géneros, reparto y póster.",
          })}
        </p>

        {/* Form de búsqueda. Enter dentro de cualquier input dispara
            search — mismo gesto que en Plex/Jellyfin. */}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleSearch();
          }}
          className="flex flex-wrap items-end gap-2"
        >
          <div className="flex-1 min-w-[180px]">
            <label
              htmlFor="identify-query"
              className="block text-xs font-medium text-text-muted"
            >
              {t("identify.queryLabel", { defaultValue: "Título" })}
            </label>
            <input
              id="identify-query"
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="mt-1 w-full rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
            />
          </div>
          <div className="w-24">
            <label
              htmlFor="identify-year"
              className="block text-xs font-medium text-text-muted"
            >
              {t("identify.yearLabel", { defaultValue: "Año" })}
            </label>
            <input
              id="identify-year"
              type="number"
              value={year}
              onChange={(e) => setYear(e.target.value)}
              placeholder="—"
              min={1900}
              max={2100}
              className="mt-1 w-full rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
            />
          </div>
          <Button type="submit" variant="primary" disabled={!query.trim()}>
            <Search className="size-4" />
            {t("identify.search", { defaultValue: "Buscar" })}
          </Button>
        </form>

        {/* Resultados. Tres estados: nunca buscó (hint), cargando, lista. */}
        <div className="min-h-[200px]">
          {!hasSearched && (
            <p className="py-8 text-center text-sm text-text-muted">
              {t("identify.hintNoSearch", {
                defaultValue:
                  "Escribe el título correcto y pulsa Buscar para ver candidatos en TMDb.",
              })}
            </p>
          )}

          {hasSearched && candidatesQ.isLoading && (
            <div className="flex justify-center py-8">
              <Spinner size="md" />
            </div>
          )}

          {hasSearched && candidatesQ.isError && (
            <div className="flex items-start gap-2 rounded-[--radius-md] border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
              <AlertCircle className="size-4 shrink-0" />
              <span>
                {t("identify.errorSearch", {
                  defaultValue:
                    "No se ha podido consultar TMDb. Revisa la configuración del provider o intenta de nuevo.",
                })}
              </span>
            </div>
          )}

          {hasSearched && candidatesQ.isSuccess && candidatesQ.data.length === 0 && (
            <p className="py-8 text-center text-sm text-text-muted">
              {t("identify.empty", {
                defaultValue:
                  "Ningún match. Prueba con otro título o quita el año.",
              })}
            </p>
          )}

          {hasSearched && candidatesQ.isSuccess && candidatesQ.data.length > 0 && (
            <ul
              className="grid grid-cols-1 gap-2 sm:grid-cols-2"
              role="radiogroup"
              aria-label={t("identify.candidatesAriaLabel", {
                defaultValue: "Candidatos TMDb",
              })}
            >
              {candidatesQ.data.map((candidate) => (
                <CandidateCard
                  key={`${candidate.provider}:${candidate.external_id}`}
                  candidate={candidate}
                  selected={selectedID === candidate.external_id}
                  onSelect={() => setSelectedID(candidate.external_id)}
                />
              ))}
            </ul>
          )}
        </div>

        {apply.isError && (
          <div className="flex items-start gap-2 rounded-[--radius-md] border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            <AlertCircle className="size-4 shrink-0" />
            <span>
              {t("identify.errorApply", {
                defaultValue:
                  "No se ha podido aplicar el match. Inténtalo de nuevo.",
              })}
            </span>
          </div>
        )}

        <div className="flex justify-end gap-2 border-t border-border pt-3">
          <Button variant="ghost" onClick={onClose} disabled={apply.isPending}>
            {t("common.cancel", { defaultValue: "Cancelar" })}
          </Button>
          <Button
            variant="primary"
            onClick={handleApply}
            disabled={!selectedID || apply.isPending}
          >
            {apply.isPending ? <Spinner size="sm" /> : <Check className="size-4" />}
            {t("identify.apply", { defaultValue: "Aplicar match" })}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

interface CandidateCardProps {
  candidate: IdentifyCandidate;
  selected: boolean;
  onSelect: () => void;
}

function CandidateCard({ candidate, selected, onSelect }: CandidateCardProps) {
  return (
    <li>
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        onClick={onSelect}
        className={[
          "flex w-full gap-3 rounded-[--radius-md] border p-2 text-left transition-colors",
          selected
            ? "border-accent bg-accent/10"
            : "border-border bg-bg-card hover:border-border-strong",
        ].join(" ")}
        data-testid="identify-candidate"
      >
        <div className="h-24 w-16 shrink-0 overflow-hidden rounded bg-bg-elevated">
          {candidate.poster_url ? (
            <img
              src={candidate.poster_url}
              alt=""
              className="size-full object-cover"
              loading="lazy"
            />
          ) : (
            <div className="flex size-full items-center justify-center text-xs text-text-muted">
              –
            </div>
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex items-baseline gap-1.5">
            <span className="truncate text-sm font-medium text-text">
              {candidate.title}
            </span>
            {candidate.year > 0 && (
              <span className="shrink-0 text-xs text-text-muted">
                ({candidate.year})
              </span>
            )}
          </div>
          {candidate.overview && (
            <p className="mt-1 line-clamp-3 text-xs text-text-muted">
              {candidate.overview}
            </p>
          )}
          <span className="mt-auto text-[10px] uppercase tracking-wide text-text-muted">
            {candidate.provider} · {candidate.external_id}
          </span>
        </div>
      </button>
    </li>
  );
}
