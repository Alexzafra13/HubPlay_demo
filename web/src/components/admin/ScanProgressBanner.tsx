import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { useScanProgress } from "@/api/hooks";

// ScanProgressBanner — renders one row per actively-scanning library.
// Disappears when no scan is running so the header doesn't jitter on
// every page load. The data comes from /api/v1/events SSE; the
// useScanProgress hook owns the connection lifetime so this component
// is just a thin display.
//
// We deliberately don't show a fractional progress bar: the scanner
// doesn't pre-walk to compute a total (would be slow on big trees),
// so we'd be lying about percent. A spinner + the running file count +
// the current relative path reads "alive and progressing" without
// pretending to know how much is left.

export function ScanProgressBanner() {
  const { t } = useTranslation();
  const scans = useScanProgress();
  if (scans.size === 0) return null;

  return (
    <div className="flex flex-col gap-2 rounded-[--radius-md] border border-accent/30 bg-accent/5 px-4 py-3">
      {Array.from(scans.values()).map((s) => (
        <div
          key={s.libraryId}
          className="flex flex-wrap items-center gap-3 text-sm"
        >
          <Loader2 className="h-4 w-4 flex-none animate-spin text-accent" />
          <span className="font-medium text-text-primary">
            {t("admin.scan.scanning", {
              defaultValue: "Escaneando '{{name}}'",
              name: s.libraryName,
            })}
          </span>
          <span className="text-xs text-text-muted tabular-nums">
            {t("admin.scan.fileCount", {
              defaultValue: "{{count}} archivos",
              count: s.scanned,
            })}
          </span>
          {s.currentPath && (
            <span className="ml-auto truncate font-mono text-[11px] text-text-muted/80">
              {s.currentPath}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}
