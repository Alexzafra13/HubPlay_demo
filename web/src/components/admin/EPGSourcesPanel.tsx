import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useAddEPGSource,
  useEPGCatalog,
  useLibraryEPGSources,
  useRemoveEPGSource,
  useReorderEPGSources,
} from "@/api/hooks";
import type { LibraryEPGSource } from "@/api/types";
import { Button, Spinner } from "@/components/common";

/**
 * EPGSourcesPanel — multi-provider EPG management for a livetv library.
 *
 * Renders the ordered list of attached providers with their last-refresh
 * status, plus a form at the bottom to attach another one (from the
 * curated catalog or a custom URL). The reorder arrows rewrite priority;
 * the refresher walks sources in the same order so dragging davidmuma
 * above epg.pw makes davidmuma the authoritative source for every
 * channel both can cover.
 *
 * Deliberately a plain list with ↑/↓ buttons instead of drag-and-drop.
 * A keyboard-accessible reorder surface is simpler to get right and the
 * typical library has 2-4 sources, not 40.
 */
export function EPGSourcesPanel({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const { data: catalog = [], isLoading: catalogLoading } = useEPGCatalog();
  const { data: sources = [], isLoading: sourcesLoading } =
    useLibraryEPGSources(libraryId);
  const addSource = useAddEPGSource(libraryId);
  const removeSource = useRemoveEPGSource(libraryId);
  const reorderSources = useReorderEPGSources(libraryId);

  // Form state for the "add source" row.
  const [mode, setMode] = useState<"catalog" | "custom">("catalog");
  const [catalogID, setCatalogID] = useState("");
  const [customURL, setCustomURL] = useState("");
  const [formError, setFormError] = useState("");

  const catalogByID = useMemo(
    () => new Map(catalog.map((c) => [c.id, c])),
    [catalog],
  );

  // Filter out catalog entries already attached so the operator can't
  // try to add davidmuma twice (backend would 4xx anyway, but the UX
  // is cleaner if the option just disappears).
  const attachedCatalogIDs = new Set(
    sources.map((s) => s.catalog_id).filter(Boolean),
  );
  const availableCatalog = catalog.filter(
    (c) => !attachedCatalogIDs.has(c.id),
  );

  async function handleAdd() {
    setFormError("");
    try {
      if (mode === "catalog") {
        if (!catalogID) {
          setFormError(
            t("admin.epg.pickCatalog", {
              defaultValue: "Selecciona una fuente del catálogo",
            }),
          );
          return;
        }
        await addSource.mutateAsync({ catalog_id: catalogID });
        setCatalogID("");
      } else {
        const url = customURL.trim();
        if (!url) {
          setFormError(
            t("admin.epg.emptyURL", {
              defaultValue: "Introduce una URL XMLTV",
            }),
          );
          return;
        }
        await addSource.mutateAsync({ url });
        setCustomURL("");
      }
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    }
  }

  function handleMove(index: number, direction: -1 | 1) {
    const next = [...sources];
    const target = index + direction;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    reorderSources.mutate(next.map((s) => s.id));
  }

  if (sourcesLoading || catalogLoading) {
    return (
      <div className="flex items-center gap-2 text-text-secondary text-sm py-2">
        <Spinner />
        <span>
          {t("admin.epg.loading", { defaultValue: "Cargando fuentes EPG…" })}
        </span>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-xs text-text-secondary">
          {t("admin.epg.priorityHint", {
            defaultValue:
              "Prioridad: la primera fuente gana cuando varias cubren el mismo canal.",
          })}
        </p>
      </div>

      {sources.length === 0 ? (
        <p className="text-sm text-text-secondary">
          {t("admin.epg.empty", {
            defaultValue:
              "Aún no hay fuentes EPG. Añade una del catálogo o pega una URL XMLTV para que la guía aparezca en la app.",
          })}
        </p>
      ) : (
        <ol className="space-y-2" aria-label={t("admin.epg.listLabel", {
          defaultValue: "Fuentes EPG configuradas, en orden de prioridad",
        })}>
          {sources.map((src, i) => (
            <SourceRow
              key={src.id}
              source={src}
              catalogName={
                src.catalog_id
                  ? catalogByID.get(src.catalog_id)?.name ?? src.catalog_id
                  : ""
              }
              canMoveUp={i > 0}
              canMoveDown={i < sources.length - 1}
              onMoveUp={() => handleMove(i, -1)}
              onMoveDown={() => handleMove(i, 1)}
              onRemove={() => removeSource.mutate(src.id)}
              isBusy={
                removeSource.isPending || reorderSources.isPending
              }
            />
          ))}
        </ol>
      )}

      <div className="border-t border-border pt-3 space-y-2">
        <div className="flex gap-2 text-xs">
          <button
            type="button"
            onClick={() => setMode("catalog")}
            className={`px-2 py-1 rounded ${
              mode === "catalog"
                ? "bg-accent text-white"
                : "bg-bg-elevated text-text-secondary"
            }`}
          >
            {t("admin.epg.fromCatalog", { defaultValue: "Del catálogo" })}
          </button>
          <button
            type="button"
            onClick={() => setMode("custom")}
            className={`px-2 py-1 rounded ${
              mode === "custom"
                ? "bg-accent text-white"
                : "bg-bg-elevated text-text-secondary"
            }`}
          >
            {t("admin.epg.custom", { defaultValue: "URL personalizada" })}
          </button>
        </div>

        {mode === "catalog" ? (
          <div className="flex gap-2">
            <select
              value={catalogID}
              onChange={(e) => setCatalogID(e.target.value)}
              className="flex-1 px-3 py-2 text-sm bg-bg-card border border-border rounded"
              aria-label={t("admin.epg.catalogLabel", {
                defaultValue: "Catálogo de fuentes EPG",
              })}
            >
              <option value="">
                {t("admin.epg.pickCatalogPlaceholder", {
                  defaultValue: "— Selecciona una fuente —",
                })}
              </option>
              {availableCatalog.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name} ({c.language})
                </option>
              ))}
            </select>
            <Button
              size="sm"
              onClick={handleAdd}
              isLoading={addSource.isPending}
              disabled={!catalogID || availableCatalog.length === 0}
            >
              {t("admin.epg.add", { defaultValue: "Añadir" })}
            </Button>
          </div>
        ) : (
          <div className="flex gap-2">
            <input
              type="url"
              value={customURL}
              onChange={(e) => setCustomURL(e.target.value)}
              placeholder="https://example.com/guide.xml"
              className="flex-1 px-3 py-2 text-sm bg-bg-card border border-border rounded"
              aria-label={t("admin.epg.customURLLabel", {
                defaultValue: "URL XMLTV personalizada",
              })}
            />
            <Button
              size="sm"
              onClick={handleAdd}
              isLoading={addSource.isPending}
              disabled={!customURL.trim()}
            >
              {t("admin.epg.add", { defaultValue: "Añadir" })}
            </Button>
          </div>
        )}

        {catalogID && mode === "catalog" ? (
          <p className="text-xs text-text-secondary">
            {catalogByID.get(catalogID)?.description}
          </p>
        ) : null}

        {formError ? (
          <p className="text-xs text-error" role="alert">
            {formError}
          </p>
        ) : null}
      </div>
    </div>
  );
}

interface SourceRowProps {
  source: LibraryEPGSource;
  catalogName: string;
  canMoveUp: boolean;
  canMoveDown: boolean;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onRemove: () => void;
  isBusy: boolean;
}

function SourceRow({
  source,
  catalogName,
  canMoveUp,
  canMoveDown,
  onMoveUp,
  onMoveDown,
  onRemove,
  isBusy,
}: SourceRowProps) {
  const { t } = useTranslation();
  const label = catalogName || source.url;

  let statusBadge;
  if (source.last_status === "ok") {
    statusBadge = (
      <span className="text-xs text-green-500">
        ✓ {source.last_program_count}{" "}
        {t("admin.epg.programs", { defaultValue: "programas" })} ·{" "}
        {source.last_channel_count}{" "}
        {t("admin.epg.channelsMatched", { defaultValue: "canales" })}
      </span>
    );
  } else if (source.last_status === "error") {
    statusBadge = (
      <span
        className="text-xs text-error"
        title={source.last_error}
      >
        ✗ {t("admin.epg.error", { defaultValue: "error" })}
      </span>
    );
  } else {
    statusBadge = (
      <span className="text-xs text-text-secondary">
        {t("admin.epg.neverRefreshed", {
          defaultValue: "nunca refrescada",
        })}
      </span>
    );
  }

  return (
    <li className="flex items-center gap-2 p-2 bg-bg-card rounded border border-border/50">
      <div className="flex flex-col gap-0.5">
        <button
          type="button"
          onClick={onMoveUp}
          disabled={!canMoveUp || isBusy}
          className="text-text-secondary disabled:opacity-30 hover:text-text-primary text-xs"
          aria-label={t("admin.epg.moveUp", { defaultValue: "Subir prioridad" })}
        >
          ▲
        </button>
        <button
          type="button"
          onClick={onMoveDown}
          disabled={!canMoveDown || isBusy}
          className="text-text-secondary disabled:opacity-30 hover:text-text-primary text-xs"
          aria-label={t("admin.epg.moveDown", { defaultValue: "Bajar prioridad" })}
        >
          ▼
        </button>
      </div>

      <div className="flex-1 min-w-0">
        <div className="text-sm text-text-primary truncate" title={label}>
          {label}
        </div>
        <div className="flex items-center gap-2">
          {statusBadge}
          {source.catalog_id ? (
            <span className="text-xs text-text-secondary">
              · {t("admin.epg.catalogEntry", { defaultValue: "del catálogo" })}
            </span>
          ) : (
            <span
              className="text-xs text-text-secondary truncate"
              title={source.url}
            >
              · {source.url}
            </span>
          )}
        </div>
      </div>

      <Button
        variant="ghost"
        size="sm"
        onClick={onRemove}
        disabled={isBusy}
        aria-label={t("admin.epg.remove", { defaultValue: "Eliminar fuente" })}
      >
        ×
      </Button>
    </li>
  );
}
