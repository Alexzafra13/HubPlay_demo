import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import type { DownloadJob } from "@/api/types";

function pct(j: DownloadJob): number {
  if (!j.bytes_total || j.bytes_total <= 0) return 0;
  return Math.min(100, Math.round((j.bytes_done / j.bytes_total) * 100));
}

const STATUS: Record<DownloadJob["status"], [string, string]> = {
  queued: ["downloads.queued", "En cola"],
  downloading: ["downloads.downloading", "Descargando"],
  copying: ["downloads.copying", "Copiando a la biblioteca"],
  completed: ["downloads.completed", "Completado"],
  failed: ["downloads.failed", "Error"],
};

// DownloadsPanel shows active/finished download-to-library jobs with live
// progress (polled). Hidden when there are none.
export function DownloadsPanel() {
  const { t } = useTranslation();
  const { data } = useQuery({
    queryKey: ["torrent-downloads"],
    queryFn: () => api.listDownloads(),
    refetchInterval: 3000,
    retry: false,
  });
  const jobs = data ?? [];
  if (jobs.length === 0) return null;

  return (
    <section className="flex flex-col gap-3 rounded-xl border border-border bg-bg-card p-5">
      <h2 className="text-lg font-semibold text-text-primary">
        {t("downloads.title", { defaultValue: "Descargas" })}
      </h2>
      <ul className="flex flex-col gap-3">
        {jobs.map((j) => (
          <li key={j.id} className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-3 text-[13px]">
              <span className="truncate text-text-primary" title={j.name}>
                {j.name || "…"}
              </span>
              <span className="shrink-0 text-[11px] text-text-muted">
                {t(STATUS[j.status][0], { defaultValue: STATUS[j.status][1] })} · {pct(j)}%
              </span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded bg-bg-hover">
              <div
                className={`h-full rounded ${j.status === "failed" ? "bg-error" : "bg-accent"}`}
                style={{ width: `${j.status === "completed" ? 100 : pct(j)}%` }}
              />
            </div>
            {j.error ? <span className="text-[11px] text-error">{j.error}</span> : null}
          </li>
        ))}
      </ul>
    </section>
  );
}
