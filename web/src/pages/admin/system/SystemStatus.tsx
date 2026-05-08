import { useSystemStats } from "@/api/hooks";
import type { SystemStats } from "@/api/types";
import { Badge, Spinner, Button, EmptyState } from "@/components/common";
import { AuthKeysPanel } from "@/components/admin/AuthKeysPanel";
import { useTranslation } from "react-i18next";

import { SystemSettingsSection } from "./SystemSettingsSection";

// Refresh cadence for the live stats. 30s matches the original
// behaviour and is frequent enough to feel live without flooding the
// dir-walk on the backend (image cache + transcode cache are
// filesystem walks).
const REFETCH_MS = 30_000;

// SystemStatus — admin Sistema page.
//
// The previous iteration was a 4-column bento of stat cards split
// into five sections (Servidor / Streaming / Runtime / Almacenamiento
// / Bibliotecas). It read as machine-generated and exposed runtime
// data (CPU cores, raw memory) that an admin rarely consults.
//
// This redesign trades the bento for editorial blocks:
//   1. Identity strip — single line: version + uptime + clock.
//   2. Estado — three health rows (DB / FFmpeg / URL pública)
//      rendered as horizontal pills with copy that explains the
//      problem when something is degraded, not just "Operativo".
//   3. Conexión — bind address + base URL summary, leading directly
//      into the editable settings section so an admin missing a
//      URL pública sees the input the same scroll height.
//   4. Streaming — sessions meter (active / max), HW accel state.
//   5. Almacenamiento — three horizontal bars at the same scale so
//      the eye registers which directory grew the most. Total size
//      summed in the section header.
//   6. Configuración — runtime-editable settings (existing panel).
//   7. Avanzado — destructive ops kept at the bottom with a banner.

// ─── Helpers ────────────────────────────────────────────────────────

function formatUptime(seconds: number): string {
  if (!seconds || seconds < 0) return "—";
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0 || days > 0) parts.push(`${hours}h`);
  parts.push(`${minutes}m`);
  return parts.join(" ");
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

function formatServerTime(iso: string): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString();
}

// ─── Page ──────────────────────────────────────────────────────────

export default function SystemStatus() {
  const { t } = useTranslation();
  const {
    data: stats,
    isLoading,
    isFetching,
    error,
    dataUpdatedAt,
    refetch,
  } = useSystemStats({ refetchInterval: REFETCH_MS });

  if (isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (error || !stats) {
    return (
      <EmptyState
        title={t("admin.system.unreachable")}
        description={error?.message ?? t("admin.system.unableToReach")}
        action={
          <Button onClick={() => refetch()} isLoading={isFetching}>
            {t("admin.system.refresh")}
          </Button>
        }
      />
    );
  }

  return (
    <div className="flex flex-col gap-12">
      <IdentityStrip
        stats={stats}
        dataUpdatedAt={dataUpdatedAt}
        isFetching={isFetching}
        onRefresh={() => refetch()}
      />

      <HealthSection stats={stats} />

      <ConnectionSection stats={stats} />

      <StreamingSection stats={stats} />

      <StorageSection stats={stats} />

      <SystemSettingsSection />

      {/* Advanced — destructive / power-user actions kept at the
          bottom of the page so the eye doesn't land on them by
          default. */}
      <section className="flex flex-col gap-3 pt-6 border-t border-border-subtle">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
          {t("admin.system.sectionAdvanced")}
        </h3>
        <div
          role="note"
          className="rounded-[--radius-md] border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-warning"
        >
          {t("admin.advanced.warning")}
        </div>
        <AuthKeysPanel />
      </section>
    </div>
  );
}

// ─── Identity strip ────────────────────────────────────────────────

interface IdentityStripProps {
  stats: SystemStats;
  dataUpdatedAt: number;
  isFetching: boolean;
  onRefresh: () => void;
}

function IdentityStrip({ stats, dataUpdatedAt, isFetching, onRefresh }: IdentityStripProps) {
  const { t } = useTranslation();
  const allHealthy = stats.database.ok && stats.ffmpeg.found;
  return (
    <header className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
      <span
        aria-hidden
        className="h-2 w-2 rounded-full"
        style={{
          background: allHealthy ? "var(--color-success)" : "var(--color-error)",
        }}
      />
      <span className="font-semibold text-text-primary">
        HubPlay {stats.server.version}
      </span>
      <span className="text-text-muted">·</span>
      <span className="text-text-secondary">
        {t("admin.summary.uptime", { uptime: formatUptime(stats.server.uptime_seconds) })}
      </span>
      <span className="text-text-muted">·</span>
      <span className="text-text-secondary tabular-nums">
        {formatServerTime(stats.server.server_time)} {stats.server.timezone}
      </span>
      <span className="ml-auto flex items-center gap-3 text-xs text-text-muted">
        {dataUpdatedAt > 0 && (
          <span>
            {t("admin.system.updated", {
              time: new Date(dataUpdatedAt).toLocaleTimeString(),
            })}
          </span>
        )}
        <Button
          variant="ghost"
          size="sm"
          onClick={onRefresh}
          isLoading={isFetching}
        >
          {isFetching ? t("admin.system.refreshing") : t("admin.system.refresh")}
        </Button>
      </span>
    </header>
  );
}

// ─── Estado ─────────────────────────────────────────────────────────

function HealthSection({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();

  const rows: HealthRowProps[] = [
    {
      label: t("admin.system.database"),
      ok: stats.database.ok,
      okText: t("admin.systemHealth.dbOk", {
        defaultValue: "Operativo · {{size}}",
        size: formatBytes(stats.database.size_bytes),
      }),
      errorText: stats.database.error ?? t("admin.system.degraded"),
      detail: stats.database.path,
    },
    {
      label: "FFmpeg",
      ok: stats.ffmpeg.found,
      okText: t("admin.systemHealth.ffmpegOk", {
        defaultValue: "Encontrado · {{path}}",
        path: stats.ffmpeg.path,
      }),
      errorText: t("admin.system.ffmpegMissing"),
      detail: stats.ffmpeg.found
        ? undefined
        : t("admin.systemHealth.ffmpegMissingHint", {
            defaultValue:
              "Instala ffmpeg en el host o monta el binario en el contenedor — sin él no hay transcodificación.",
          }),
    },
    {
      label: t("admin.system.baseURL"),
      ok: !!stats.server.base_url,
      okText: stats.server.base_url || "—",
      errorText: t("admin.systemHealth.baseURLMissing", {
        defaultValue: "Sin configurar",
      }),
      detail: stats.server.base_url
        ? undefined
        : t("admin.system.baseURLUnset"),
    },
  ];

  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-base font-semibold text-text-primary">
        {t("admin.systemHealth.title", { defaultValue: "Estado" })}
      </h2>
      <ul className="flex flex-col divide-y divide-border-subtle rounded-[--radius-lg] border border-border bg-bg-card">
        {rows.map((r) => (
          <HealthRow key={r.label} {...r} />
        ))}
      </ul>
    </section>
  );
}

interface HealthRowProps {
  label: string;
  ok: boolean;
  okText: string;
  errorText: string;
  detail?: string;
}

function HealthRow({ label, ok, okText, errorText, detail }: HealthRowProps) {
  return (
    <li className="flex flex-wrap items-center gap-3 px-5 py-3.5 text-sm">
      <span className="min-w-[110px] font-medium text-text-primary">
        {label}
      </span>
      <Badge variant={ok ? "success" : "error"}>
        {ok ? "OK" : "FALLO"}
      </Badge>
      <span className="text-text-secondary truncate flex-1 min-w-0 font-mono text-xs">
        {ok ? okText : errorText}
      </span>
      {detail && (
        <span className="basis-full pl-[126px] text-xs text-text-muted">
          {detail}
        </span>
      )}
    </li>
  );
}

// ─── Conexión ──────────────────────────────────────────────────────

function ConnectionSection({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-base font-semibold text-text-primary">
        {t("admin.systemConnection.title", { defaultValue: "Conexión" })}
      </h2>
      <p className="text-sm text-text-muted">
        {t("admin.systemConnection.hint", {
          defaultValue:
            "El servidor escucha en la dirección de abajo y se identifica externamente con la URL pública (la editas en Configuración).",
        })}
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <ConnectionField
          label={t("admin.system.bindAddress")}
          value={stats.server.bind_address || "—"}
          hint={t("admin.systemConnection.bindHint", {
            defaultValue: "Configurada vía hubplay.yaml o $HUBPLAY_SERVER_HOST/PORT.",
          })}
        />
        <ConnectionField
          label={t("admin.system.baseURL")}
          value={stats.server.base_url || "—"}
          hint={
            stats.server.base_url
              ? undefined
              : t("admin.system.baseURLUnset")
          }
        />
      </div>
    </section>
  );
}

function ConnectionField({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="flex flex-col gap-1 rounded-[--radius-md] border border-border bg-bg-card px-4 py-3">
      <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
        {label}
      </span>
      <span className="text-base font-mono text-text-primary break-all">
        {value}
      </span>
      {hint && <span className="text-xs text-text-muted">{hint}</span>}
    </div>
  );
}

// ─── Streaming ──────────────────────────────────────────────────────

function StreamingSection({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const { transcode_sessions_active, transcode_sessions_max } = stats.streaming;
  // The meter runs from 0..max. When max == 0 (unlimited config) the
  // bar reads "ilimitado" and we just show the active count without
  // a denominator — a meter pinned at "X / ∞" looks broken.
  const max = transcode_sessions_max;
  const active = transcode_sessions_active;
  const pct = max > 0 ? Math.min(100, (active / max) * 100) : 0;

  const accelEnabled = stats.ffmpeg.hw_accel_enabled;
  const selected = stats.ffmpeg.hw_accel_selected;
  const available = stats.ffmpeg.hw_accels_available ?? [];
  let accelLabel: string;
  let accelTone: "success" | "warning" | "default";
  if (!accelEnabled) {
    accelLabel = t("admin.system.hwAccelDisabledLabel");
    accelTone = "default";
  } else if (!selected || selected === "none") {
    accelLabel = t("admin.system.hwAccelNone");
    accelTone = "warning";
  } else {
    accelLabel = selected.toUpperCase();
    accelTone = "success";
  }

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-base font-semibold text-text-primary">
        {t("admin.system.sectionStreaming")}
      </h2>
      <div className="grid gap-4 lg:grid-cols-2">
        {/* Sessions meter */}
        <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
          <div className="flex items-baseline justify-between">
            <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
              {t("admin.system.activeTranscodes")}
            </span>
            <span className="text-xs text-text-muted tabular-nums">
              {max > 0
                ? t("admin.system.transcodeSlots", { active, max })
                : t("admin.system.transcodeUnlimited", { active })}
            </span>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-3xl font-semibold text-text-primary tabular-nums">
              {active}
            </span>
            {max > 0 && (
              <span className="text-base text-text-muted tabular-nums">
                / {max}
              </span>
            )}
          </div>
          {max > 0 && (
            <div className="h-1 w-full overflow-hidden rounded-full bg-bg-elevated">
              <div
                className="h-full transition-all"
                style={{
                  width: `${pct}%`,
                  background:
                    pct < 70
                      ? "var(--color-success)"
                      : pct < 95
                        ? "var(--color-warning)"
                        : "var(--color-error)",
                }}
              />
            </div>
          )}
        </div>

        {/* Hardware acceleration */}
        <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
          <div className="flex items-baseline justify-between">
            <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
              {t("admin.system.hwAccelSelected")}
            </span>
            {available.length > 0 && (
              <span className="text-xs text-text-muted truncate">
                {t("admin.system.hwAccelAvailable")}:{" "}
                {available.map((a) => a.toUpperCase()).join(", ")}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <span className="text-2xl font-semibold text-text-primary">
              {accelLabel}
            </span>
            <Badge
              variant={
                accelTone === "success"
                  ? "success"
                  : accelTone === "warning"
                    ? "warning"
                    : "default"
              }
            >
              {accelEnabled
                ? t("admin.system.hwAccelEnabled", { defaultValue: "Activado" })
                : t("admin.system.hwAccelDisabledLabel")}
            </Badge>
          </div>
          <p className="text-xs text-text-muted">
            {accelEnabled
              ? stats.ffmpeg.hw_accel_encoder
                ? `${t("admin.system.hwAccelEncoder")}: ${stats.ffmpeg.hw_accel_encoder}`
                : t("admin.system.hwAccelNoneHint")
              : t("admin.system.hwAccelDisabledPointer")}
          </p>
        </div>
      </div>
    </section>
  );
}

// ─── Almacenamiento ─────────────────────────────────────────────────

function StorageSection({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const s = stats.storage;
  const dbBytes = stats.database.size_bytes;
  const total = (s.image_dir_bytes ?? 0) + (s.transcode_cache_bytes ?? 0) + dbBytes;
  // Bars share the same denominator (the largest of the three) so
  // the eye registers relative size at a glance — a 2 GiB image
  // cache vs a 30 MiB DB reads as a real ratio, not three numbers.
  const denom = Math.max(s.image_dir_bytes, s.transcode_cache_bytes, dbBytes, 1);
  const rows = [
    {
      label: t("admin.system.imageDir"),
      bytes: s.image_dir_bytes,
      path: s.image_dir_path,
    },
    {
      label: t("admin.system.transcodeCache"),
      bytes: s.transcode_cache_bytes,
      path: s.transcode_cache_path,
    },
    {
      label: t("admin.system.databaseSize"),
      bytes: dbBytes,
      path: stats.database.path,
    },
  ];
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-base font-semibold text-text-primary">
          {t("admin.system.sectionStorage")}
        </h2>
        <span className="text-sm text-text-muted tabular-nums">
          {t("admin.systemStorage.total", {
            defaultValue: "Total {{size}}",
            size: formatBytes(total),
          })}
        </span>
      </div>
      <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
        {rows.map((r) => (
          <StorageRow
            key={r.label}
            label={r.label}
            bytes={r.bytes}
            path={r.path}
            denom={denom}
          />
        ))}
      </div>
    </section>
  );
}

interface StorageRowProps {
  label: string;
  bytes: number;
  path?: string;
  denom: number;
}

function StorageRow({ label, bytes, path, denom }: StorageRowProps) {
  const pct = denom > 0 ? (bytes / denom) * 100 : 0;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-3 text-sm">
        <span className="font-medium text-text-primary">{label}</span>
        <span className="tabular-nums text-text-secondary">
          {formatBytes(bytes)}
        </span>
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-bg-elevated">
        <div
          className="h-full bg-accent transition-all"
          style={{ width: `${pct}%` }}
        />
      </div>
      {path && (
        <span className="text-[11px] font-mono text-text-muted truncate">
          {path}
        </span>
      )}
    </div>
  );
}
