import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Download, Upload } from "lucide-react";
import { api } from "@/api/client";
import { Button } from "@/components/common";

// BackupPanel — admin "Copia de seguridad" surface, mounted under
// /admin/system → Avanzado. Two operations:
//
//   - Download: hits /admin/system/backup, which runs `VACUUM INTO`
//     server-side and streams a consistent .db snapshot. We turn it
//     into a Blob, mint an object URL and click an anchor to trigger
//     the browser download. The filename is server-set (UTC stamp);
//     we mirror that locally as a fallback in case the server
//     header is stripped by a proxy.
//
//   - Restore: file picker → POST multipart to
//     /admin/system/backup/restore. The server stages the file as
//     `<dbdir>/.pending-restore.db`; the actual swap happens at the
//     next process boot. We surface the "restart to apply" hint
//     directly in the success message because there's no live swap
//     mode and the operator might otherwise expect immediate effect.

export function BackupPanel() {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [downloading, setDownloading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [status, setStatus] = useState<
    | { kind: "success"; text: string }
    | { kind: "error"; text: string }
    | null
  >(null);

  const handleDownload = async () => {
    setDownloading(true);
    setStatus(null);
    try {
      const blob = await api.downloadBackup();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `hubplay-backup-${new Date()
        .toISOString()
        .slice(0, 10)
        .replace(/-/g, "")}.db`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setStatus({
        kind: "error",
        text:
          err instanceof Error
            ? err.message
            : t("admin.backup.downloadFailed", {
                defaultValue: "No pudimos generar el backup.",
              }),
      });
    } finally {
      setDownloading(false);
    }
  };

  const handleFilePicked = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    // Always reset the input so picking the same file twice in a row
    // still fires onChange — without this, a retry after a failed
    // upload would silently no-op.
    e.target.value = "";
    if (!file) return;

    const ok = window.confirm(
      t("admin.backup.restoreConfirm", {
        defaultValue:
          "El backup sustituirá la base de datos actual al reiniciar HubPlay. La copia anterior se guardará al lado con un nombre con fecha. ¿Continuar?",
      }),
    );
    if (!ok) return;

    setUploading(true);
    setStatus(null);
    try {
      const result = await api.restoreBackup(file);
      setStatus({
        kind: "success",
        text: t("admin.backup.restoreStaged", {
          defaultValue:
            "Backup preparado ({{size}}). Se aplicará al reiniciar HubPlay.",
          size: formatBytes(result.size_bytes),
        }),
      });
    } catch (err) {
      setStatus({
        kind: "error",
        text:
          err instanceof Error
            ? err.message
            : t("admin.backup.restoreFailed", {
                defaultValue: "No pudimos preparar el backup.",
              }),
      });
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
      <div>
        <h3 className="text-sm font-semibold text-text-primary">
          {t("admin.backup.title", { defaultValue: "Copia de seguridad" })}
        </h3>
        <p className="mt-1 text-xs text-text-muted leading-relaxed">
          {t("admin.backup.subtitle", {
            defaultValue:
              "Descarga una copia consistente de la base de datos o restaura una previa. La restauración se aplica al reiniciar HubPlay; la BD anterior se guarda al lado con marca de tiempo.",
          })}
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void handleDownload()}
          isLoading={downloading}
          disabled={uploading}
        >
          <Download className="-ml-0.5 mr-1.5 size-3.5" />
          {t("admin.backup.download", { defaultValue: "Descargar backup" })}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => fileInputRef.current?.click()}
          isLoading={uploading}
          disabled={downloading}
        >
          <Upload className="-ml-0.5 mr-1.5 size-3.5" />
          {t("admin.backup.restore", { defaultValue: "Restaurar copia" })}
        </Button>
        <input
          ref={fileInputRef}
          type="file"
          accept=".db,application/octet-stream"
          onChange={(e) => void handleFilePicked(e)}
          className="hidden"
        />
      </div>

      {status && (
        <p
          role="status"
          className={[
            "rounded-md border px-3 py-2 text-xs",
            status.kind === "success"
              ? "border-success/30 bg-success/10 text-success"
              : "border-error/30 bg-error/10 text-error",
          ].join(" ")}
        >
          {status.text}
        </p>
      )}
    </div>
  );
}

function formatBytes(n: number): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i <= 1 ? `${Math.round(v)} ${units[i]}` : `${v.toFixed(1)} ${units[i]}`;
}
