// LibraryCard — one row inside its section group on the LibrariesAdmin
// page.
//
// The card calls its own mutation hooks (scan / refresh-meta / refresh-
// images / refresh-M3U / refresh-EPG) so the page doesn't have to
// drill 12 props per card. Toast-style feedback for the longer-running
// refresh operations is bubbled up via `onShowMessage` because the
// banner that renders the message is a single shared element above the
// list, not per-row.
//
// Edit + delete don't fire here — they're owned by the page (the page
// hosts the modal state). The card just notifies via callbacks.

import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { Badge, Button } from "@/components/common";
import {
  useScanLibrary,
  useRefreshLibraryImages,
  useRefreshM3U,
  useRefreshEPG,
} from "@/api/hooks";
import type { Library } from "@/api/types";
import { originLabel, originTitle, scanStatusVariant } from "./helpers";

export interface RefreshMessage {
  type: "success" | "error";
  text: string;
  libId: string;
}

// LibraryDiskInfo — peso de la biblioteca + stats del mount donde
// vive. Llega desde GET /admin/system/storage/disks, agrupado por
// disco fisico. La parent page (LibrariesAdmin) construye un
// map<libId, LibraryDiskInfo> a partir del response. Es undefined
// cuando:
//   - el endpoint todavia esta cargando (1ª render)
//   - la library no tiene paths (livetv M3U remoto)
//   - el endpoint fallo (degrade gracefully)
export interface LibraryDiskInfo {
  sizeBytes: number;
  fileCount: number;
  mount: string;
  mountTotalBytes: number;
  mountUsedBytes: number;
  mountUsedPercent: number;
}

interface LibraryCardProps {
  library: Library;
  diskInfo?: LibraryDiskInfo;
  onEdit: (lib: Library) => void;
  onDelete: (lib: Library) => void;
  onShowMessage: (msg: RefreshMessage) => void;
}

export function LibraryCard({
  library: lib,
  diskInfo,
  onEdit,
  onDelete,
  onShowMessage,
}: LibraryCardProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const scanLibrary = useScanLibrary();
  const refreshImages = useRefreshLibraryImages();
  const refreshM3U = useRefreshM3U();
  const refreshEPG = useRefreshEPG();

  const isLivetv = lib.content_type === "livetv";

  return (
    <li className="rounded-[--radius-lg] border border-border bg-bg-card overflow-hidden">
      <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-start sm:gap-4">
        <div className="min-w-0 flex-1">
          <h3 className="font-medium text-text-primary truncate">{lib.name}</h3>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-text-muted min-w-0">
            <span className="shrink-0">
              <span className="tabular-nums text-text-secondary">{lib.item_count}</span>{" "}
              {isLivetv
                ? t("admin.libraries.channelsLower", { defaultValue: "canales" })
                : t("admin.libraries.itemsLower", { defaultValue: "elementos" })}
            </span>
            {originLabel(lib) && (
              <>
                <span aria-hidden className="h-0.5 w-0.5 rounded-full bg-border shrink-0" />
                <span className="font-mono truncate max-w-full" title={originTitle(lib)}>
                  {originLabel(lib)}
                </span>
              </>
            )}
            {!isLivetv && lib.scan_status && (
              <>
                <span aria-hidden className="h-0.5 w-0.5 rounded-full bg-border shrink-0" />
                <Badge variant={scanStatusVariant(lib.scan_status)}>
                  {lib.scan_status}
                </Badge>
              </>
            )}
          </div>
        </div>
        {/* Mobile (default): actions wrap onto a new line below the
            name and break across rows on narrow screens. The vertical
            separator is hidden on mobile because once buttons wrap, a
            1px line in the middle of a row reads as visual noise. */}
        <div className="flex flex-wrap items-center gap-1 sm:shrink-0">
          {isLivetv ? (
            // ── Live TV row: refresh M3U + refresh EPG ──
            // Filesystem scan and metadata/image refresh don't apply
            // here; showing them would just yield dead buttons, so
            // we route to the IPTV-specific actions instead.
            <>
              <Button
                variant="secondary"
                size="sm"
                isLoading={refreshM3U.isPending && refreshM3U.variables === lib.id}
                onClick={() =>
                  refreshM3U.mutate(lib.id, {
                    onSuccess: (data) =>
                      onShowMessage({
                        type: "success",
                        text: t("admin.libraries.refreshM3USuccess", {
                          defaultValue: `{{count}} canales importados`,
                          count: data.channels_imported,
                        }),
                        libId: lib.id,
                      }),
                    onError: () =>
                      onShowMessage({
                        type: "error",
                        text: t("admin.libraries.refreshM3UFailed", {
                          defaultValue: "No se pudo refrescar el M3U.",
                        }),
                        libId: lib.id,
                      }),
                  })
                }
                title={lib.m3u_url || undefined}
              >
                {t("admin.libraries.refreshM3U", { defaultValue: "Actualizar canales" })}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                isLoading={refreshEPG.isPending && refreshEPG.variables === lib.id}
                disabled={!lib.epg_url}
                onClick={() =>
                  refreshEPG.mutate(lib.id, {
                    onSuccess: (data) =>
                      onShowMessage({
                        type: "success",
                        text: t("admin.libraries.refreshEPGSuccess", {
                          defaultValue: `{{count}} programas importados`,
                          count: data.programs_imported,
                        }),
                        libId: lib.id,
                      }),
                    onError: () =>
                      onShowMessage({
                        type: "error",
                        text: t("admin.libraries.refreshEPGFailed", {
                          defaultValue: "No se pudo refrescar la guía EPG.",
                        }),
                        libId: lib.id,
                      }),
                  })
                }
                title={
                  lib.epg_url ||
                  t("admin.libraries.noEPGURL", {
                    defaultValue: "No hay URL EPG configurada en esta biblioteca.",
                  })
                }
              >
                {t("admin.libraries.refreshEPG", { defaultValue: "Actualizar EPG" })}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => navigate(`/admin/libraries/${lib.id}`)}
              >
                {t("admin.libraries.manage", { defaultValue: "Gestionar" })}
              </Button>
            </>
          ) : (
            // ── Regular media library: scan + metadata + images ──
            <>
              <Button
                variant="secondary"
                size="sm"
                isLoading={
                  scanLibrary.isPending &&
                  scanLibrary.variables?.id === lib.id &&
                  !scanLibrary.variables?.refreshMetadata
                }
                disabled={lib.scan_status === "scanning"}
                onClick={() =>
                  scanLibrary.mutate(
                    { id: lib.id },
                    {
                      onSuccess: () =>
                        onShowMessage({
                          type: "success",
                          text: t("admin.libraries.scanStarted", {
                            defaultValue:
                              "Escaneo iniciado. La biblioteca se actualizará en segundo plano.",
                          }),
                          libId: lib.id,
                        }),
                      onError: () =>
                        onShowMessage({
                          type: "error",
                          text: t("admin.libraries.scanFailed", {
                            defaultValue: "No se pudo iniciar el escaneo.",
                          }),
                          libId: lib.id,
                        }),
                    },
                  )
                }
              >
                {t("admin.libraries.scan")}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                isLoading={
                  scanLibrary.isPending &&
                  scanLibrary.variables?.id === lib.id &&
                  !!scanLibrary.variables?.refreshMetadata
                }
                disabled={lib.scan_status === "scanning"}
                onClick={() =>
                  scanLibrary.mutate({ id: lib.id, refreshMetadata: true })
                }
                title={t("admin.libraries.refreshMetadataTooltip")}
              >
                {t("admin.libraries.refreshMetadata")}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                isLoading={
                  refreshImages.isPending &&
                  refreshImages.variables?.libraryId === lib.id
                }
                disabled={lib.scan_status === "scanning"}
                onClick={() =>
                  refreshImages.mutate(
                    { libraryId: lib.id },
                    {
                      onSuccess: (data) =>
                        onShowMessage({
                          type: "success",
                          text: t("admin.libraries.refreshImagesSuccess", {
                            count: data.updated,
                          }),
                          libId: lib.id,
                        }),
                      onError: () =>
                        onShowMessage({
                          type: "error",
                          text: t("admin.libraries.refreshImagesFailed"),
                          libId: lib.id,
                        }),
                    },
                  )
                }
              >
                {t("admin.libraries.refreshImages")}
              </Button>
            </>
          )}
          <span aria-hidden className="mx-1 hidden h-5 w-px bg-border sm:inline-block" />
          <Button variant="ghost" size="sm" onClick={() => onEdit(lib)}>
            {t("common.edit")}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="text-text-muted hover:text-error"
            onClick={() => onDelete(lib)}
          >
            {t("common.delete")}
          </Button>
        </div>
      </div>
      {/* Disk usage row — peso de la biblioteca (SQL SUM) + ocupacion
          del disco fisico donde vive (gopsutil statfs). Solo se
          renderiza cuando el endpoint /admin/system/storage/disks ha
          devuelto datos para esta library. Vive en su propia row
          al fondo del card con un fondo bg-bg-base muted para
          diferenciarse de la cabecera (no compite con el nombre +
          acciones de arriba). */}
      {diskInfo && <DiskUsageRow info={diskInfo} t={t} />}
    </li>
  );
}

// formatBytes compacto. "8.2 TB" / "320 GB" / "0 B". Local al
// LibraryCard - no merece su propio helpers.ts para 12 LOC.
function formatBytesCompact(n: number): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i <= 1 ? `${Math.round(v)} ${units[i]}` : `${v.toFixed(1)} ${units[i]}`;
}

function DiskUsageRow({
  info,
  t,
}: {
  info: LibraryDiskInfo;
  t: (k: string, opts?: Record<string, unknown>) => string;
}) {
  const pct = info.mountUsedPercent;
  // Severity para la barra del mount (no de la biblioteca):
  // - verde < 75% del disco usado
  // - ambar 75-90
  // - rojo  >= 90 (¡toca borrar cosas!)
  const tone =
    pct < 75 ? "var(--color-success)"
      : pct < 90 ? "var(--color-warning)"
        : "var(--color-error)";
  return (
    <div className="border-t border-border-subtle bg-bg-base/40 px-4 py-2.5">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-[11px]">
        <span className="flex items-baseline gap-1.5">
          <span className="text-text-muted">
            {t("admin.libraries.diskWeight", { defaultValue: "Peso:" })}
          </span>
          <span className="font-medium text-text-secondary tabular-nums">
            {formatBytesCompact(info.sizeBytes)}
          </span>
          {info.fileCount > 0 && (
            <span className="text-text-muted">
              {t("admin.libraries.fileCount", {
                defaultValue: "({{n}} ficheros)",
                n: info.fileCount.toLocaleString(),
              })}
            </span>
          )}
        </span>
        <span className="flex items-baseline gap-1.5 min-w-0">
          <span className="text-text-muted">
            {t("admin.libraries.diskMount", { defaultValue: "Disco:" })}
          </span>
          <span
            className="font-mono truncate max-w-[20ch] text-text-secondary"
            title={info.mount}
          >
            {info.mount}
          </span>
          <span className="text-text-muted">
            {t("admin.libraries.diskUsedOfTotal", {
              defaultValue: "{{used}} / {{total}} ({{pct}}%)",
              used: formatBytesCompact(info.mountUsedBytes),
              total: formatBytesCompact(info.mountTotalBytes),
              pct: pct.toFixed(0),
            })}
          </span>
        </span>
      </div>
      <div className="mt-1.5 h-0.5 w-full overflow-hidden rounded-full bg-bg-elevated">
        <div
          className="h-full transition-all"
          style={{ width: `${Math.min(100, pct)}%`, background: tone }}
        />
      </div>
    </div>
  );
}
