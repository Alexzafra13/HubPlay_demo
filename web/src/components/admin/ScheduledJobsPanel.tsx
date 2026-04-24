import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useRunScheduledJobNow,
  useScheduledJobs,
  useUpsertScheduledJob,
} from "@/api/hooks";
import type { IPTVScheduledJob, IPTVScheduledJobKind } from "@/api/types";
import { Button, Spinner } from "@/components/common";

/**
 * ScheduledJobsPanel — admin surface for automated IPTV refreshes.
 *
 * Renders two rows (M3U and EPG) with:
 *   - an enable/disable toggle,
 *   - an interval dropdown (1/3/6/12/24/72/168 h),
 *   - the last-run timestamp + status badge,
 *   - a "Run now" button that fires the refresh synchronously.
 *
 * Saves the interval/enabled pair together when either changes so the
 * admin doesn't hunt for a "Save" button — same ergonomics as the
 * EPG sources reorder surface (each action persists on its own).
 */
export function ScheduledJobsPanel({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const { data: jobs = [], isLoading } = useScheduledJobs(libraryId);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-6">
        <Spinner size="md" />
      </div>
    );
  }
  if (jobs.length === 0) {
    return (
      <p className="text-sm text-text-secondary py-2">
        {t("admin.schedule.noJobs", {
          defaultValue: "Sin tareas programadas disponibles.",
        })}
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <header className="space-y-1">
        <h3 className="text-sm font-semibold text-text-primary">
          {t("admin.schedule.title", {
            defaultValue: "Tareas automáticas",
          })}
        </h3>
        <p className="text-xs text-text-secondary">
          {t("admin.schedule.description", {
            defaultValue:
              "Refresca el M3U y la guía EPG cada X horas sin intervención.",
          })}
        </p>
      </header>
      <ul className="space-y-2" role="list">
        {jobs.map((job) => (
          <ScheduledJobRow key={job.kind} libraryId={libraryId} job={job} />
        ))}
      </ul>
    </div>
  );
}

const INTERVAL_OPTIONS: Array<{ value: number; labelKey: string; fallback: string }> = [
  { value: 1, labelKey: "admin.schedule.every.1h", fallback: "Cada 1 h" },
  { value: 3, labelKey: "admin.schedule.every.3h", fallback: "Cada 3 h" },
  { value: 6, labelKey: "admin.schedule.every.6h", fallback: "Cada 6 h" },
  { value: 12, labelKey: "admin.schedule.every.12h", fallback: "Cada 12 h" },
  { value: 24, labelKey: "admin.schedule.every.24h", fallback: "Cada 24 h" },
  { value: 72, labelKey: "admin.schedule.every.72h", fallback: "Cada 3 días" },
  { value: 168, labelKey: "admin.schedule.every.168h", fallback: "Cada semana" },
];

function ScheduledJobRow({
  libraryId,
  job,
}: {
  libraryId: string;
  job: IPTVScheduledJob;
}) {
  const { t } = useTranslation();
  const upsert = useUpsertScheduledJob(libraryId);
  const runNow = useRunScheduledJobNow(libraryId);
  const [error, setError] = useState("");

  // No local interval mirror: the dropdown reads from job.interval_hours
  // directly. React Query refetches after the mutation resolves and the
  // new value flows through as a prop, so the row stays in sync without
  // a set-state-in-effect cascade.
  const kindLabel = jobKindLabel(t, job.kind);

  async function save(next: { interval_hours: number; enabled: boolean }) {
    setError("");
    try {
      await upsert.mutateAsync({
        kind: job.kind,
        data: next,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function handleRunNow() {
    setError("");
    try {
      await runNow.mutateAsync(job.kind);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <li className="flex flex-col gap-2 rounded-md border border-border bg-bg-card/60 p-3 sm:flex-row sm:items-center sm:gap-3">
      <div className="flex-1 min-w-0 space-y-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-text-primary">
            {kindLabel}
          </span>
          <StatusBadge job={job} />
        </div>
        <LastRunLine job={job} />
        {error ? (
          <p className="text-xs text-error" role="alert">
            {error}
          </p>
        ) : null}
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          <input
            type="checkbox"
            checked={job.enabled}
            disabled={upsert.isPending}
            onChange={(e) =>
              save({
                interval_hours: job.interval_hours,
                enabled: e.target.checked,
              })
            }
            className="h-4 w-4 rounded border-border text-accent focus:ring-accent"
            aria-label={t("admin.schedule.toggle", {
              defaultValue: "Activar {{kind}}",
              kind: kindLabel,
            })}
          />
          <span>
            {job.enabled
              ? t("admin.schedule.enabled", { defaultValue: "Activo" })
              : t("admin.schedule.disabled", { defaultValue: "Inactivo" })}
          </span>
        </label>

        <select
          value={job.interval_hours}
          disabled={upsert.isPending || !job.enabled}
          onChange={(e) => {
            const next = Number(e.target.value);
            save({ interval_hours: next, enabled: job.enabled });
          }}
          className="rounded border border-border bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent focus:outline-none disabled:opacity-60"
          aria-label={t("admin.schedule.intervalLabel", {
            defaultValue: "Intervalo de {{kind}}",
            kind: kindLabel,
          })}
        >
          {INTERVAL_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {t(opt.labelKey, { defaultValue: opt.fallback })}
            </option>
          ))}
        </select>

        <Button
          variant="secondary"
          size="sm"
          onClick={handleRunNow}
          disabled={runNow.isPending}
        >
          {runNow.isPending
            ? t("admin.schedule.running", { defaultValue: "Ejecutando…" })
            : t("admin.schedule.runNow", { defaultValue: "Ejecutar ahora" })}
        </Button>
      </div>
    </li>
  );
}

function jobKindLabel(
  t: (key: string, opts?: Record<string, unknown>) => string,
  kind: IPTVScheduledJobKind,
): string {
  if (kind === "m3u_refresh") {
    return t("admin.schedule.kind.m3u", {
      defaultValue: "Refrescar M3U",
    });
  }
  return t("admin.schedule.kind.epg", {
    defaultValue: "Refrescar EPG",
  });
}

function StatusBadge({ job }: { job: IPTVScheduledJob }) {
  const { t } = useTranslation();
  if (job.last_status === "error") {
    return (
      <span className="rounded bg-error/10 px-1.5 py-0.5 text-[11px] font-medium text-error">
        {t("admin.schedule.status.error", { defaultValue: "Error" })}
      </span>
    );
  }
  if (job.last_status === "ok") {
    return (
      <span className="rounded bg-success/10 px-1.5 py-0.5 text-[11px] font-medium text-success">
        {t("admin.schedule.status.ok", { defaultValue: "OK" })}
      </span>
    );
  }
  return (
    <span className="rounded bg-text-muted/10 px-1.5 py-0.5 text-[11px] font-medium text-text-muted">
      {t("admin.schedule.status.neverRun", { defaultValue: "Sin ejecutar" })}
    </span>
  );
}

function LastRunLine({ job }: { job: IPTVScheduledJob }) {
  const { t } = useTranslation();
  if (!job.last_run_at) {
    return (
      <p className="text-xs text-text-muted">
        {t("admin.schedule.lastRun.never", {
          defaultValue: "Aún no se ha ejecutado.",
        })}
      </p>
    );
  }
  const when = formatRelative(job.last_run_at);
  if (job.last_status === "error" && job.last_error) {
    return (
      <p className="text-xs text-error truncate" title={job.last_error}>
        {t("admin.schedule.lastRun.errored", {
          defaultValue: "Falló {{when}}: {{error}}",
          when,
          error: job.last_error,
        })}
      </p>
    );
  }
  return (
    <p className="text-xs text-text-secondary">
      {t("admin.schedule.lastRun.ok", {
        defaultValue: "Última ejecución: {{when}}",
        when,
      })}
    </p>
  );
}

// formatRelative renders a short "hace 3 h" / "hace 12 min" string
// from an RFC3339 timestamp. Pure function; no locale awareness
// because the rest of the admin UI sticks to Spanish today.
function formatRelative(isoTimestamp: string): string {
  const then = Date.parse(isoTimestamp);
  if (Number.isNaN(then)) return isoTimestamp;
  const diffMs = Date.now() - then;
  if (diffMs < 0) return new Date(then).toLocaleString();
  const minutes = Math.floor(diffMs / 60_000);
  if (minutes < 1) return "ahora mismo";
  if (minutes < 60) return `hace ${minutes} min`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `hace ${hours} h`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `hace ${days} día${days === 1 ? "" : "s"}`;
  return new Date(then).toLocaleString();
}
