import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { api } from "@/api/client";
import { useEventStream } from "@/hooks/useEventStream";
import type { DownloadJob } from "@/api/types";

const QK = ["torrent-downloads"];

function pct(j: DownloadJob): number {
  if (!j.bytes_total || j.bytes_total <= 0) return 0;
  return Math.min(100, Math.round((j.bytes_done / j.bytes_total) * 100));
}

function isTerminal(s: DownloadJob["status"]): boolean {
  return s === "completed" || s === "failed";
}

// upsertJob fusiona el snapshot empujado por SSE en la lista cacheada (por id).
function upsertJob(prev: DownloadJob[] | undefined, job: DownloadJob): DownloadJob[] {
  const list = prev ? [...prev] : [];
  const i = list.findIndex((j) => j.id === job.id);
  if (i >= 0) list[i] = job;
  else list.push(job);
  return list;
}

const STATUS: Record<DownloadJob["status"], [string, string]> = {
  queued: ["downloads.queued", "En cola"],
  downloading: ["downloads.downloading", "Descargando"],
  copying: ["downloads.copying", "Copiando a la biblioteca"],
  completed: ["downloads.completed", "Completado"],
  failed: ["downloads.failed", "Error"],
};

// DownloadsPanel muestra los jobs de descarga-a-biblioteca con progreso en
// vivo (push SSE, sin polling). El fetch inicial siembra el estado; cada
// cambio llega por el stream global y se fusiona en la caché de React Query.
// Oculto cuando no hay ninguno.
export function DownloadsPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();

  const { data } = useQuery({
    queryKey: QK,
    queryFn: () => api.listDownloads(),
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Push en vivo: el backend emite event.TorrentDownload en cada transición.
  useEventStream("torrent.download", (raw) => {
    try {
      const { data: job } = JSON.parse(raw) as { data: DownloadJob };
      if (!job?.id) return;
      qc.setQueryData<DownloadJob[]>(QK, (prev) => upsertJob(prev, job));
    } catch {
      // payload mal formado — ignorar sin tumbar el stream
    }
  });

  const dismiss = useMutation({
    mutationFn: (id: string) => api.dismissDownload(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: QK }),
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
              <div className="flex shrink-0 items-center gap-2">
                <span className="text-[11px] text-text-muted">
                  {t(STATUS[j.status][0], { defaultValue: STATUS[j.status][1] })} · {pct(j)}%
                </span>
                {isTerminal(j.status) ? (
                  <button
                    type="button"
                    onClick={() => dismiss.mutate(j.id)}
                    disabled={dismiss.isPending}
                    aria-label={t("downloads.dismiss", { defaultValue: "Descartar" })}
                    title={t("downloads.dismiss", { defaultValue: "Descartar" })}
                    className="flex size-5 items-center justify-center rounded text-text-muted transition-colors hover:bg-bg-hover hover:text-text-primary disabled:opacity-50"
                  >
                    <X className="size-3.5" />
                  </button>
                ) : null}
              </div>
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
