import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Globe, Lock, Trash2, Plus } from "lucide-react";

import {
  useCorsOrigins,
  useAddCorsOrigin,
  useDeleteCorsOrigin,
} from "@/api/hooks";
import { Button, EmptyState, Input, Spinner } from "@/components/common";

// CorsOriginsPanel — owner-only management de orígenes CORS añadidos
// en runtime via el panel admin (PR4 feature).
//
// Layout:
//   - Sección "Statics (YAML)" con candado y read-only.
//   - Sección "Dynamics (DB)" con nombre, nota, autor, fecha + botón
//     eliminar por fila.
//   - Formulario "Añadir origen" abajo: input para el origen + input
//     para la nota opcional + botón añadir.
//
// El backend valida el formato (https/http, sin path, sin wildcards,
// etc.) y devuelve un mensaje de error legible que el panel
// muestra inline. No replicamos esa validación cliente — el error
// del backend es la fuente de verdad y evita drift entre los dos.

export function CorsOriginsPanel() {
  const { t } = useTranslation();
  const { data, isLoading, error } = useCorsOrigins();
  const add = useAddCorsOrigin();
  const del = useDeleteCorsOrigin();

  const [newOrigin, setNewOrigin] = useState("");
  const [newNote, setNewNote] = useState("");
  const [feedback, setFeedback] = useState<{
    type: "success" | "error";
    text: string;
  } | null>(null);

  function clearFeedback() {
    setFeedback(null);
  }

  async function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    if (!newOrigin.trim()) return;
    clearFeedback();
    try {
      await add.mutateAsync({ origin: newOrigin.trim(), note: newNote.trim() });
      setNewOrigin("");
      setNewNote("");
      setFeedback({
        type: "success",
        text: t("admin.cors.addedFeedback", { defaultValue: "Origen añadido." }),
      });
    } catch (err) {
      // El backend ya envía un mensaje legible — mostramos el suyo,
      // no traducimos a uno genérico. Caer a un texto i18n sólo si
      // el error no trae mensaje.
      const msg = err instanceof Error && err.message
        ? err.message
        : t("admin.cors.addError", { defaultValue: "No se pudo añadir el origen." });
      setFeedback({ type: "error", text: msg });
    }
  }

  async function handleDelete(origin: string) {
    clearFeedback();
    if (!confirm(t("admin.cors.deleteConfirm", { origin, defaultValue: `Eliminar ${origin}?` }))) {
      return;
    }
    try {
      await del.mutateAsync(origin);
      setFeedback({
        type: "success",
        text: t("admin.cors.removedFeedback", { defaultValue: "Origen eliminado." }),
      });
    } catch (err) {
      const msg = err instanceof Error && err.message
        ? err.message
        : t("admin.cors.deleteError", { defaultValue: "No se pudo eliminar el origen." });
      setFeedback({ type: "error", text: msg });
    }
  }

  return (
    <section
      className="rounded-[--radius-lg] border border-border bg-bg-elevated p-4 sm:p-6"
      aria-labelledby="cors-panel-title"
    >
      <header className="mb-4 flex items-start gap-2">
        <Globe size={18} className="text-accent mt-0.5 shrink-0" aria-hidden />
        <div>
          <h2 id="cors-panel-title" className="text-base font-semibold text-text-primary">
            {t("admin.cors.title", { defaultValue: "Orígenes CORS permitidos" })}
          </h2>
          <p className="text-sm text-text-muted mt-1">
            {t("admin.cors.hint", {
              defaultValue:
                "Frontends externos que pueden hablar con esta API. Los del YAML son inmutables; los añadidos aquí surten efecto al instante.",
            })}
          </p>
        </div>
      </header>

      {feedback && (
        <div
          role="alert"
          className={`mb-3 rounded-md border px-3 py-2 text-sm ${
            feedback.type === "success"
              ? "border-green-700 bg-green-900/30 text-green-200"
              : "border-red-700 bg-red-900/30 text-red-200"
          }`}
        >
          {feedback.text}
        </div>
      )}

      {isLoading && <Spinner />}

      {error && (
        <EmptyState
          title={t("admin.cors.loadErrorTitle", { defaultValue: "No se pudo cargar la lista" })}
          description={error.message}
        />
      )}

      {data && (
        <div className="flex flex-col gap-5">
          {/* Statics (YAML) */}
          <div>
            <h3 className="text-sm font-medium text-text-secondary mb-2 flex items-center gap-1.5">
              <Lock size={13} aria-hidden />
              {t("admin.cors.staticsTitle", { defaultValue: "Desde YAML (inmutables)" })}
            </h3>
            {data.statics.length === 0 ? (
              <p className="text-xs text-text-muted">
                {t("admin.cors.staticsEmpty", { defaultValue: "Ninguno." })}
              </p>
            ) : (
              <ul className="flex flex-col gap-1">
                {data.statics.map((o) => (
                  <li
                    key={o}
                    className="rounded border border-border bg-bg-base px-3 py-1.5 text-sm font-mono text-text-secondary"
                  >
                    {o}
                  </li>
                ))}
              </ul>
            )}
          </div>

          {/* Dynamics (DB) */}
          <div>
            <h3 className="text-sm font-medium text-text-secondary mb-2">
              {t("admin.cors.dynamicsTitle", { defaultValue: "Añadidos en runtime" })}
            </h3>
            {data.dynamics.length === 0 ? (
              <p className="text-xs text-text-muted">
                {t("admin.cors.dynamicsEmpty", {
                  defaultValue: "Aún no has añadido ninguno.",
                })}
              </p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {data.dynamics.map((e) => (
                  <li
                    key={e.origin}
                    className="flex items-center justify-between gap-3 rounded border border-border bg-bg-base px-3 py-2 text-sm"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="font-mono truncate">{e.origin}</p>
                      {(e.note || e.created_at) && (
                        <p className="mt-0.5 text-xs text-text-muted">
                          {e.note && <span>{e.note}</span>}
                          {e.note && e.created_at && <span> · </span>}
                          {e.created_at && (
                            <span>{new Date(e.created_at).toLocaleString()}</span>
                          )}
                        </p>
                      )}
                    </div>
                    <button
                      type="button"
                      onClick={() => handleDelete(e.origin)}
                      disabled={del.isPending}
                      aria-label={t("common.remove", { defaultValue: "Eliminar" })}
                      className="shrink-0 rounded p-1 text-text-muted hover:bg-bg-hover hover:text-red-400 transition-colors"
                    >
                      <Trash2 size={14} aria-hidden />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {/* Add form */}
          <form onSubmit={handleAdd} className="flex flex-col gap-2">
            <h3 className="text-sm font-medium text-text-secondary">
              {t("admin.cors.addTitle", { defaultValue: "Añadir origen" })}
            </h3>
            <div className="flex flex-wrap items-end gap-2">
              <Input
                value={newOrigin}
                onChange={(e) => setNewOrigin(e.target.value)}
                placeholder="https://app.example.com"
                aria-label={t("admin.cors.originLabel", { defaultValue: "Origen" })}
                className="flex-1 min-w-[260px] font-mono text-sm"
                required
              />
              <Input
                value={newNote}
                onChange={(e) => setNewNote(e.target.value)}
                placeholder={t("admin.cors.notePlaceholder", {
                  defaultValue: "Nota (opcional)",
                })}
                aria-label={t("admin.cors.noteLabel", { defaultValue: "Nota" })}
                className="flex-1 min-w-[180px] text-sm"
              />
              <Button
                type="submit"
                isLoading={add.isPending}
                disabled={!newOrigin.trim()}
              >
                <Plus size={14} className="mr-1" aria-hidden />
                {t("admin.cors.addCta", { defaultValue: "Añadir" })}
              </Button>
            </div>
            <p className="text-xs text-text-muted">
              {t("admin.cors.addExample", {
                defaultValue:
                  "Formato: scheme://host[:port]. Sólo http o https; sin path ni wildcards.",
              })}
            </p>
          </form>
        </div>
      )}
    </section>
  );
}
