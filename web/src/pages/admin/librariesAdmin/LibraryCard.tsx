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
import { Badge, Button } from "@/components/common";
import {
  useScanLibrary,
  useRefreshLibraryImages,
  useRefreshM3U,
  useRefreshEPG,
} from "@/api/hooks";
import { LivetvAdminPanel } from "@/components/admin/LivetvAdminPanel";
import type { Library } from "@/api/types";
import { originLabel, originTitle, scanStatusVariant } from "./helpers";

export interface RefreshMessage {
  type: "success" | "error";
  text: string;
  libId: string;
}

interface LibraryCardProps {
  library: Library;
  onEdit: (lib: Library) => void;
  onDelete: (lib: Library) => void;
  onShowMessage: (msg: RefreshMessage) => void;
}

export function LibraryCard({
  library: lib,
  onEdit,
  onDelete,
  onShowMessage,
}: LibraryCardProps) {
  const { t } = useTranslation();
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
                onClick={() => scanLibrary.mutate({ id: lib.id })}
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
      {isLivetv && (
        <div className="border-t border-border bg-bg-card/40 px-4 py-3">
          <LivetvAdminPanel libraryId={lib.id} totalChannels={lib.item_count} />
        </div>
      )}
    </li>
  );
}
