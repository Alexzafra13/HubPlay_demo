// MetadataEditorDialog — modal admin para editar metadatos a mano
// (sin pasar por TMDb). Estilo Jellyfin "Edit metadata": formulario
// con los campos importantes pre-rellenados; al guardar bloquea el
// item para que un siguiente "Refresh metadata" no pise la edición.
//
// Distinto de IdentifyDialog: éste no consulta TMDb. Si el operador
// quiere el match correcto + todos los campos derivados (cast, póster,
// géneros), usa Identify. Si sólo quiere corregir el overview o el
// año, usa este editor.

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertCircle, Check, Lock } from "lucide-react";

import { useUpdateItemMetadata } from "@/api/hooks";
import type { ItemDetail } from "@/api/types";
import { Button, Spinner } from "@/components/common";
import { Modal } from "@/components/common/Modal";

interface Props {
  isOpen: boolean;
  onClose: () => void;
  item: ItemDetail;
}

export function MetadataEditorDialog({ isOpen, onClose, item }: Props) {
  const { t } = useTranslation();

  const [title, setTitle] = useState(item.title ?? "");
  const [originalTitle, setOriginalTitle] = useState(item.original_title ?? "");
  const [year, setYear] = useState<string>(item.year ? String(item.year) : "");
  const [overview, setOverview] = useState(item.overview ?? "");
  const [tagline, setTagline] = useState(item.tagline ?? "");
  const [seededOpen, setSeededOpen] = useState(false);

  // Re-siembra cuando el modal pasa de cerrado a abierto. El item
  // puede haber cambiado (otro identify mientras tanto) — los inputs
  // reflejan el estado más reciente cada vez que se abre.
  if (isOpen && !seededOpen) {
    setSeededOpen(true);
    setTitle(item.title ?? "");
    setOriginalTitle(item.original_title ?? "");
    setYear(item.year ? String(item.year) : "");
    setOverview(item.overview ?? "");
    setTagline(item.tagline ?? "");
  } else if (!isOpen && seededOpen) {
    setSeededOpen(false);
  }

  const update = useUpdateItemMetadata(item.id);

  const handleSave = useCallback(async () => {
    // Sólo enviamos los campos que han cambiado respecto al item
    // original — un PATCH con todos los campos también funcionaría
    // pero gastar un PATCH del overview cuando sólo se tocó el year
    // dispararía writes innecesarios en la tabla metadata.
    const patch: {
      title?: string;
      original_title?: string;
      year?: number;
      overview?: string;
      tagline?: string;
    } = {};
    if (title !== (item.title ?? "")) patch.title = title;
    if (originalTitle !== (item.original_title ?? "")) {
      patch.original_title = originalTitle;
    }
    const parsedYear = year ? parseInt(year, 10) : 0;
    if (parsedYear !== (item.year ?? 0) && !Number.isNaN(parsedYear)) {
      patch.year = parsedYear;
    }
    if (overview !== (item.overview ?? "")) patch.overview = overview;
    if (tagline !== (item.tagline ?? "")) patch.tagline = tagline;

    if (Object.keys(patch).length === 0) {
      onClose();
      return;
    }
    await update.mutateAsync(patch);
    onClose();
  }, [item, title, originalTitle, year, overview, tagline, update, onClose]);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("metadataEditor.title", { defaultValue: "Editar metadatos" })}
      size="lg"
    >
      <div className="flex flex-col gap-3">
        <div className="flex items-start gap-2 rounded-[--radius-md] border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
          <Lock className="h-3.5 w-3.5 shrink-0" />
          <span>
            {t("metadataEditor.lockHint", {
              defaultValue:
                "Al guardar, el item queda bloqueado: los siguientes refreshes automáticos no pisarán tus cambios.",
            })}
          </span>
        </div>

        <Field
          id="md-title"
          label={t("metadataEditor.fieldTitle", { defaultValue: "Título" })}
          value={title}
          onChange={setTitle}
        />
        <Field
          id="md-original-title"
          label={t("metadataEditor.fieldOriginalTitle", {
            defaultValue: "Título original",
          })}
          value={originalTitle}
          onChange={setOriginalTitle}
        />
        <Field
          id="md-year"
          label={t("metadataEditor.fieldYear", { defaultValue: "Año" })}
          value={year}
          onChange={setYear}
          type="number"
          width="w-32"
        />
        <Field
          id="md-tagline"
          label={t("metadataEditor.fieldTagline", { defaultValue: "Tagline" })}
          value={tagline}
          onChange={setTagline}
        />
        <div className="flex flex-col gap-1.5">
          <label
            htmlFor="md-overview"
            className="text-xs font-medium text-text-muted"
          >
            {t("metadataEditor.fieldOverview", { defaultValue: "Sinopsis" })}
          </label>
          <textarea
            id="md-overview"
            value={overview}
            onChange={(e) => setOverview(e.target.value)}
            rows={6}
            className="rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
          />
        </div>

        {update.isError && (
          <div className="flex items-start gap-2 rounded-[--radius-md] border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span>
              {t("metadataEditor.errorGeneric", {
                defaultValue: "No se ha podido guardar. Inténtalo de nuevo.",
              })}
            </span>
          </div>
        )}

        <div className="flex justify-end gap-2 border-t border-border pt-3">
          <Button variant="ghost" onClick={onClose} disabled={update.isPending}>
            {t("common.cancel", { defaultValue: "Cancelar" })}
          </Button>
          <Button variant="primary" onClick={handleSave} disabled={update.isPending}>
            {update.isPending ? <Spinner size="sm" /> : <Check className="h-4 w-4" />}
            {t("metadataEditor.save", { defaultValue: "Guardar" })}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

interface FieldProps {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  width?: string;
}

function Field({ id, label, value, onChange, type = "text", width = "" }: FieldProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-xs font-medium text-text-muted">
        {label}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`${width || "w-full"} rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30`}
      />
    </div>
  );
}
