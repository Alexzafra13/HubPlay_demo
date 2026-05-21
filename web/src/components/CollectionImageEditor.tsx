// CollectionImageEditor — modal admin para cambiar el póster y el
// fondo de una colección (saga TMDb). Mismo patrón que el editor
// de logo de canal: dos pestañas (poster / backdrop), cada una con
//   - Preview de la imagen actual (TMDb o override aplicado).
//   - Input de URL externa + botón "Aplicar URL".
//   - Botón "Subir archivo" (multipart, validaciones server-side).
//   - Botón "Restaurar imagen de TMDb" (borra el override).
//
// El backend conserva la URL TMDb intocada; los overrides son una
// capa por encima que se borra con la papelera y vuelve al original.

import { useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertCircle, Check, RotateCcw, Upload } from "lucide-react";

import {
  useAvailableCollectionImages,
  useClearCollectionImage,
  useSetCollectionImageURL,
  useUploadCollectionImage,
} from "@/api/hooks";
import type { CollectionDetail } from "@/api/types";
import { Button, Spinner } from "@/components/common";
import { Modal } from "@/components/common/Modal";

interface Props {
  isOpen: boolean;
  onClose: () => void;
  collection: CollectionDetail;
}

type ImageType = "poster" | "backdrop";

export function CollectionImageEditor({ isOpen, onClose, collection }: Props) {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<ImageType>("poster");
  const [urlInput, setUrlInput] = useState("");
  // Bumpea cuando guardamos para refrescar el <img> del preview sin
  // recargar la página (la URL del proxy es estable; el navegador
  // serviría la cacheada sin el ?v).
  const [previewBuster, setPreviewBuster] = useState(0);

  const setURL = useSetCollectionImageURL(collection.id);
  const uploadFile = useUploadCollectionImage(collection.id);
  const clear = useClearCollectionImage(collection.id);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Browse TMDb. Lazy — sólo se dispara cuando el modal está abierto;
  // cambia de query key al cambiar de tab para que el cache de la otra
  // pestaña no se pise.
  const available = useAvailableCollectionImages(collection.id, activeTab, {
    enabled: isOpen,
  });

  const handleApplyURL = useCallback(async () => {
    const trimmed = urlInput.trim();
    if (!trimmed) return;
    await setURL.mutateAsync({ type: activeTab, url: trimmed });
    setUrlInput("");
    setPreviewBuster(Date.now());
  }, [activeTab, setURL, urlInput]);

  const handleFile = useCallback(
    async (file: File) => {
      await uploadFile.mutateAsync({ type: activeTab, file });
      setPreviewBuster(Date.now());
    },
    [activeTab, uploadFile],
  );

  const handleClear = useCallback(async () => {
    await clear.mutateAsync(activeTab);
    setPreviewBuster(Date.now());
  }, [activeTab, clear]);

  // Click en una imagen del browser TMDb → la persistimos como
  // override URL. Reusa el flujo de SetURL — no descargamos el
  // archivo, sólo guardamos la URL TMDb. El proxy del browser ya
  // cachea la imagen del lado CDN.
  const handlePickAvailable = useCallback(
    async (url: string) => {
      await setURL.mutateAsync({ type: activeTab, url });
      setPreviewBuster(Date.now());
    },
    [activeTab, setURL],
  );

  // URL para la preview. Para poster el aspect ratio del card es 2:3;
  // para backdrop, 16:9. El src lleva el cache-buster cuando ya hemos
  // hecho una mutación en esta sesión.
  const sourceURL = activeTab === "poster" ? collection.poster_url : collection.backdrop_url;
  const previewSrc =
    sourceURL && previewBuster > 0
      ? `${sourceURL}${sourceURL.includes("?") ? "&" : "?"}v=${previewBuster}`
      : sourceURL;

  const busy = setURL.isPending || uploadFile.isPending || clear.isPending;
  const anyError = setURL.error || uploadFile.error || clear.error;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("collectionImage.title", { defaultValue: "Imágenes de la colección" })}
      size="lg"
    >
      <div className="flex flex-col gap-4">
        {/* Tabs Poster / Fondo */}
        <div className="flex gap-2 border-b border-border">
          {(["poster", "backdrop"] as ImageType[]).map((t2) => (
            <button
              key={t2}
              type="button"
              onClick={() => setActiveTab(t2)}
              className={[
                "rounded-t-md px-3 py-2 text-sm font-medium transition-colors",
                activeTab === t2
                  ? "border-b-2 border-accent text-text"
                  : "text-text-muted hover:text-text",
              ].join(" ")}
            >
              {t2 === "poster"
                ? t("collectionImage.tabPoster", { defaultValue: "Póster" })
                : t("collectionImage.tabBackdrop", { defaultValue: "Fondo" })}
            </button>
          ))}
        </div>

        {/* Preview */}
        <div
          className={[
            "overflow-hidden rounded-[--radius-md] border border-border bg-bg-elevated",
            activeTab === "poster" ? "mx-auto aspect-[2/3] w-48" : "aspect-video w-full",
          ].join(" ")}
        >
          {previewSrc ? (
            <img
              src={previewSrc}
              alt={collection.name}
              className="size-full object-cover"
            />
          ) : (
            <div className="flex size-full items-center justify-center text-text-muted">
              {t("collectionImage.noImage", {
                defaultValue: "Sin imagen (TMDb no provee esta para esta colección).",
              })}
            </div>
          )}
        </div>

        {/* Browse TMDb — todas las imágenes que TMDb tiene de la saga,
            filtradas por tipo activo. Click sobre cualquiera la
            aplica como override URL (cero descarga: usamos la URL
            TMDb que es estable). Estado vacío cuando TMDb no provee
            o cuando el provider no está configurado. */}
        <div className="flex flex-col gap-2">
          <p className="text-xs font-medium text-text-muted">
            {t("collectionImage.availableLabel", {
              defaultValue: "Imágenes disponibles en TMDb",
            })}
          </p>
          {available.isLoading ? (
            <div className="flex justify-center py-6">
              <Spinner size="md" />
            </div>
          ) : available.isError ? (
            <p className="text-xs text-text-muted">
              {t("collectionImage.availableError", {
                defaultValue:
                  "No se ha podido contactar con TMDb. Reintenta más tarde o usa URL/Subir.",
              })}
            </p>
          ) : !available.data || available.data.length === 0 ? (
            <p className="text-xs text-text-muted">
              {t("collectionImage.availableEmpty", {
                defaultValue:
                  "TMDb no tiene imágenes alternativas para esta colección.",
              })}
            </p>
          ) : (
            <div
              className={[
                "grid gap-2 max-h-[260px] overflow-y-auto",
                activeTab === "poster"
                  ? "grid-cols-3 sm:grid-cols-4 md:grid-cols-5"
                  : "grid-cols-2 sm:grid-cols-3",
              ].join(" ")}
            >
              {available.data.map((img) => (
                <button
                  key={img.url}
                  type="button"
                  onClick={() => handlePickAvailable(img.url)}
                  disabled={busy}
                  className={[
                    "group relative overflow-hidden rounded-[--radius-md] border border-border bg-bg-elevated transition-transform hover:scale-[1.03] focus:outline-none focus:ring-2 focus:ring-accent",
                    activeTab === "poster" ? "aspect-[2/3]" : "aspect-video",
                  ].join(" ")}
                  title={img.language ? `${img.language.toUpperCase()} · ${img.width}×${img.height}` : `${img.width}×${img.height}`}
                >
                  <img
                    src={img.url}
                    alt=""
                    loading="lazy"
                    className="size-full object-cover"
                  />
                  {img.language && (
                    <span className="absolute bottom-1 left-1 rounded bg-black/70 px-1 py-0 text-[9px] font-mono font-semibold uppercase text-white">
                      {img.language}
                    </span>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Separador antes del input URL — la sección de arriba es
            visualmente densa y este corte ayuda a leer las dos rutas
            como alternativas. */}
        <div className="flex items-center gap-2 text-[10px] uppercase tracking-wide text-text-muted">
          <span className="h-px flex-1 bg-border" />
          {t("collectionImage.orManual", { defaultValue: "O manualmente" })}
          <span className="h-px flex-1 bg-border" />
        </div>

        {/* URL input */}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleApplyURL();
          }}
          className="flex flex-col gap-1.5"
        >
          <label
            htmlFor="collection-image-url"
            className="text-xs font-medium text-text-muted"
          >
            {t("collectionImage.urlLabel", { defaultValue: "URL externa" })}
          </label>
          <div className="flex gap-2">
            <input
              id="collection-image-url"
              type="url"
              value={urlInput}
              onChange={(e) => setUrlInput(e.target.value)}
              placeholder="https://example.com/poster.jpg"
              className="flex-1 rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
            />
            <Button type="submit" variant="primary" disabled={!urlInput.trim() || busy}>
              {setURL.isPending ? <Spinner size="sm" /> : <Check className="size-4" />}
              {t("collectionImage.applyURL", { defaultValue: "Aplicar URL" })}
            </Button>
          </div>
        </form>

        {/* OR separator */}
        <div className="flex items-center gap-2 text-[10px] uppercase tracking-wide text-text-muted">
          <span className="h-px flex-1 bg-border" />
          {t("collectionImage.or", { defaultValue: "O" })}
          <span className="h-px flex-1 bg-border" />
        </div>

        {/* Upload */}
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-text-muted">
            {t("collectionImage.uploadLabel", {
              defaultValue: "Subir imagen (PNG, JPG, WebP — máx 10MB)",
            })}
          </label>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/png,image/jpeg,image/webp"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) {
                handleFile(file);
                e.target.value = "";
              }
            }}
            className="hidden"
          />
          <Button
            type="button"
            variant="ghost"
            onClick={() => fileInputRef.current?.click()}
            disabled={busy}
          >
            {uploadFile.isPending ? <Spinner size="sm" /> : <Upload className="size-4" />}
            {t("collectionImage.chooseFile", { defaultValue: "Elegir archivo" })}
          </Button>
        </div>

        {anyError && (
          <div className="flex items-start gap-2 rounded-[--radius-md] border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            <AlertCircle className="size-4 shrink-0" />
            <span>
              {t("collectionImage.errorGeneric", {
                defaultValue:
                  "No se ha podido aplicar el cambio. Comprueba que el archivo o la URL son válidos.",
              })}
            </span>
          </div>
        )}

        <div className="flex flex-wrap justify-between gap-2 border-t border-border pt-3">
          <Button variant="ghost" onClick={handleClear} disabled={busy}>
            {clear.isPending ? (
              <Spinner size="sm" />
            ) : (
              <RotateCcw className="size-4" />
            )}
            {t("collectionImage.reset", {
              defaultValue: "Restaurar imagen de TMDb",
            })}
          </Button>
          <Button variant="primary" onClick={onClose} disabled={busy}>
            {t("collectionImage.done", { defaultValue: "Hecho" })}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
