// ChannelLogoEditor — modal admin para personalizar el logo de un
// canal IPTV. Tres rutas:
//
//   1. URL externa — el operador pega un enlace a una imagen
//      (Wikipedia, sitio oficial, CDN). El backend lo cachea via la
//      misma maquinaria que ya proxia los tvg-logo del M3U.
//   2. Archivo subido — el operador elige un PNG/JPG/WEBP local. Se
//      guarda bajo <imageDir>/channel-logos/<id-<ts>.<ext>.
//   3. Restaurar el M3U — borra la fila de override; la próxima
//      petición del proxy vuelve al tvg-logo del playlist.
//
// La preview a la izquierda muestra el logo EFECTIVO actual via el
// endpoint /channels/{id}/logo (que ya aplica la cascada override →
// M3U → 404). Tras un save bumpeamos un cache-buster en la query
// string para forzar al browser a refetchear el bytes nuevo.

import { useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertCircle, Check, Image as ImageIcon, RotateCcw, Upload } from "lucide-react";

import {
  useClearChannelLogo,
  useSetChannelLogoURL,
  useUploadChannelLogo,
} from "@/api/hooks";
import { Button, Spinner } from "@/components/common";
import { Modal } from "@/components/common/Modal";

interface Props {
  isOpen: boolean;
  onClose: () => void;
  channelID: string;
  channelName: string;
  /** URL del proxy del canal — /api/v1/channels/{id}/logo. La
   *  preview cuelga de aquí; tras guardar le pegamos un ?v= para
   *  bustear cache local del browser. */
  proxyLogoURL: string;
  /** Iniciales + colores deterministicos del canal. Fallback cuando
   *  ningún logo (override, M3U) está disponible. */
  initials: string;
  initialsBg: string;
  initialsFg: string;
}

export function ChannelLogoEditor({
  isOpen,
  onClose,
  channelID,
  channelName,
  proxyLogoURL,
  initials,
  initialsBg,
  initialsFg,
}: Props) {
  const { t } = useTranslation();
  const [urlInput, setUrlInput] = useState("");
  const [previewBuster, setPreviewBuster] = useState(0);
  const [previewBroken, setPreviewBroken] = useState(false);
  const [seededOpen, setSeededOpen] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const setURL = useSetChannelLogoURL();
  const uploadFile = useUploadChannelLogo();
  const clear = useClearChannelLogo();

  // Re-siembra al abrir, igual que IdentifyDialog.
  if (isOpen && !seededOpen) {
    setSeededOpen(true);
    setUrlInput("");
    setPreviewBuster(0);
    setPreviewBroken(false);
  } else if (!isOpen && seededOpen) {
    setSeededOpen(false);
  }

  const refreshPreview = useCallback(() => {
    setPreviewBuster(Date.now());
    setPreviewBroken(false);
  }, []);

  const handleApplyURL = useCallback(async () => {
    if (!urlInput.trim()) return;
    await setURL.mutateAsync({ channelId: channelID, logoURL: urlInput.trim() });
    setUrlInput("");
    refreshPreview();
  }, [channelID, refreshPreview, setURL, urlInput]);

  const handleFile = useCallback(
    async (file: File) => {
      await uploadFile.mutateAsync({ channelId: channelID, file });
      refreshPreview();
    },
    [channelID, refreshPreview, uploadFile],
  );

  const handleClear = useCallback(async () => {
    await clear.mutateAsync(channelID);
    refreshPreview();
  }, [channelID, clear, refreshPreview]);

  const previewSrc =
    previewBuster > 0 ? `${proxyLogoURL}?v=${previewBuster}` : proxyLogoURL;

  const busy = setURL.isPending || uploadFile.isPending || clear.isPending;
  const anyError = setURL.error || uploadFile.error || clear.error;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("channelLogo.title", { defaultValue: "Logo del canal" })}
      size="md"
    >
      <div className="flex flex-col gap-4">
        {/* Preview + identidad del canal. */}
        <div className="flex items-center gap-3 rounded-[--radius-md] border border-border bg-bg-card p-3">
          <span
            className="flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-full text-base font-semibold"
            style={{ backgroundColor: initialsBg, color: initialsFg }}
            aria-hidden="true"
          >
            {previewBroken ? (
              <span>{initials || "?"}</span>
            ) : (
              <img
                src={previewSrc}
                alt=""
                className="size-full object-cover"
                onError={() => setPreviewBroken(true)}
              />
            )}
          </span>
          <div className="min-w-0">
            <div className="truncate text-sm font-medium text-text">{channelName}</div>
            <div className="text-xs text-text-muted">
              {t("channelLogo.previewHint", {
                defaultValue:
                  "Vista previa con el logo actualmente activo (override admin o tvg-logo del M3U).",
              })}
            </div>
          </div>
        </div>

        {/* URL externa. */}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleApplyURL();
          }}
          className="flex flex-col gap-1.5"
        >
          <label
            htmlFor="channel-logo-url"
            className="text-xs font-medium text-text-muted"
          >
            {t("channelLogo.urlLabel", { defaultValue: "URL externa" })}
          </label>
          <div className="flex gap-2">
            <input
              id="channel-logo-url"
              type="url"
              value={urlInput}
              onChange={(e) => setUrlInput(e.target.value)}
              placeholder="https://example.com/logo.png"
              className="flex-1 rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
            />
            <Button
              type="submit"
              variant="primary"
              disabled={!urlInput.trim() || busy}
            >
              {setURL.isPending ? <Spinner size="sm" /> : <Check className="size-4" />}
              {t("channelLogo.applyURL", { defaultValue: "Aplicar URL" })}
            </Button>
          </div>
        </form>

        {/* Separador con "O". */}
        <div className="flex items-center gap-2 text-[10px] uppercase tracking-wide text-text-muted">
          <span className="h-px flex-1 bg-border" />
          {t("channelLogo.or", { defaultValue: "O" })}
          <span className="h-px flex-1 bg-border" />
        </div>

        {/* Upload local. */}
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-text-muted">
            {t("channelLogo.uploadLabel", { defaultValue: "Subir imagen (PNG, JPG, WebP — máx 10MB)" })}
          </label>
          <div className="flex gap-2">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) {
                  handleFile(file);
                  // Limpia el input para que un upload del mismo
                  // archivo dos veces seguidas dispare onChange.
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
              {uploadFile.isPending ? (
                <Spinner size="sm" />
              ) : (
                <Upload className="size-4" />
              )}
              {t("channelLogo.chooseFile", { defaultValue: "Elegir archivo" })}
            </Button>
          </div>
        </div>

        {anyError && (
          <div className="flex items-start gap-2 rounded-[--radius-md] border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            <AlertCircle className="size-4 shrink-0" />
            <span>
              {t("channelLogo.errorGeneric", {
                defaultValue:
                  "No se ha podido aplicar el cambio. Comprueba que el archivo es una imagen válida o que la URL devuelve un PNG/JPG/WebP.",
              })}
            </span>
          </div>
        )}

        {/* Acciones inferiores: restaurar al M3U + cerrar. */}
        <div className="flex flex-wrap justify-between gap-2 border-t border-border pt-3">
          <Button
            type="button"
            variant="ghost"
            onClick={handleClear}
            disabled={busy}
          >
            {clear.isPending ? (
              <Spinner size="sm" />
            ) : (
              <RotateCcw className="size-4" />
            )}
            {t("channelLogo.reset", { defaultValue: "Restaurar logo del M3U" })}
          </Button>
          <Button type="button" variant="primary" onClick={onClose} disabled={busy}>
            <ImageIcon className="size-4" />
            {t("channelLogo.done", { defaultValue: "Hecho" })}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
