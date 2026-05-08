import { useEffect, useRef, useState } from "react";
import {
  Activity,
  Cpu,
  HardDrive,
  KeyRound,
  Network,
  RefreshCw,
  Settings2,
  Square,
  Zap,
} from "lucide-react";
import {
  useAdminStreamSessions,
  useKillAdminStreamSession,
  useSystemStats,
} from "@/api/hooks";
import type {
  AdminStreamSession,
  SystemRuntimeStats,
  SystemStats,
} from "@/api/types";
import { Spinner, Button, EmptyState } from "@/components/common";
import { AuthKeysPanel } from "@/components/admin/AuthKeysPanel";
import { BackupPanel } from "@/components/admin/BackupPanel";
import { SectionHeader } from "@/components/admin/SectionHeader";
import { Sparkline } from "@/components/admin/Sparkline";
import { useTranslation } from "react-i18next";

import { SystemSettingsSection } from "./SystemSettingsSection";

// Refresh cadence for the live stats. 30 s feels live without
// hammering the dir-walks (image cache + transcode cache are FS
// scans). Stream sessions poll separately at 5 s — they're cheap
// (in-memory map) and the inline "Sesiones activas" panel needs to
// look truly live or it doesn't pull its weight.
const REFETCH_MS = 30_000;

// SystemStatus — admin Sistema page.
//
// Editorial layout with live sparklines next to the metrics that
// actually move (transcode sessions, process memory). The page
// reads top-to-bottom as: who you are → is anything broken → how
// are you reachable → what's playing right now → where is the disk
// going → how do I configure it → and finally, the dangerous bits.
//
// The sparklines are populated by a client-side ring buffer that
// captures one sample per stats refetch (so ~120 samples over an
// hour). No backend changes — the goal is "feel premium" not
// "build an APM". When we want true CPU% we'll add a backend
// sampler; for now memory_alloc + sessions cover the meaningful
// axes (Go heap pressure + concurrent transcodes).

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

// ─── Metrics ring buffer ────────────────────────────────────────────

interface MetricsSample {
  /** ms epoch — unused for plotting but kept so we can render an
   *  "X samples" caption later if needed. */
  ts: number;
  sessions: number;
  memMb: number;
}

const MAX_SAMPLES = 120; // ≈ 1 h at 30 s cadence

// useMetricsHistory — captures a sample on every stats update and
// keeps the last MAX_SAMPLES in a sliding window. Pure client-side
// state: reload resets the buffer, which is fine for a "is the
// number going up or down right now" view. We use a ref-based
// dedupe so React's strict-mode double-effect doesn't double-push.
function useMetricsHistory(
  stats: SystemStats | undefined,
  dataUpdatedAt: number,
): MetricsSample[] {
  const [samples, setSamples] = useState<MetricsSample[]>([]);
  const lastTsRef = useRef(0);

  useEffect(() => {
    if (!stats || dataUpdatedAt === 0 || dataUpdatedAt === lastTsRef.current) {
      return;
    }
    lastTsRef.current = dataUpdatedAt;
    setSamples((prev) => {
      const next = prev.concat({
        ts: dataUpdatedAt,
        sessions: stats.streaming.transcode_sessions_active,
        memMb: stats.runtime.memory_alloc_mb,
      });
      return next.length > MAX_SAMPLES ? next.slice(-MAX_SAMPLES) : next;
    });
  }, [stats, dataUpdatedAt]);

  return samples;
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
  const history = useMetricsHistory(stats, dataUpdatedAt);

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
    <div className="flex flex-col gap-10">
      <IdentityStrip
        stats={stats}
        dataUpdatedAt={dataUpdatedAt}
        isFetching={isFetching}
        onRefresh={() => refetch()}
      />

      <HealthSection stats={stats} />

      <ConnectionSection stats={stats} />

      <StreamingSection stats={stats} history={history} />

      <RuntimeSection runtime={stats.runtime} history={history} />

      <StorageSection stats={stats} />

      <SettingsSection />

      <AdvancedSection />
    </div>
  );
}

// SeverityDot — coloured circle + label, replaces the larger Badge
// component in the health rows. Same information, lighter visual
// footprint, more macOS-Settings-like.
function SeverityDot({
  tone,
  label,
}: {
  tone: "success" | "warning" | "error" | "neutral";
  label: string;
}) {
  const colour =
    tone === "success"
      ? "var(--color-success)"
      : tone === "warning"
        ? "var(--color-warning)"
        : tone === "error"
          ? "var(--color-error)"
          : "var(--color-text-muted)";
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-text-secondary">
      <span
        aria-hidden
        className="h-2 w-2 rounded-full"
        style={{ background: colour }}
      />
      {label}
    </span>
  );
}

// ─── Identity strip ────────────────────────────────────────────────

interface IdentityStripProps {
  stats: SystemStats;
  dataUpdatedAt: number;
  isFetching: boolean;
  onRefresh: () => void;
}

function IdentityStrip({
  stats,
  dataUpdatedAt,
  isFetching,
  onRefresh,
}: IdentityStripProps) {
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
        {t("admin.summary.uptime", {
          uptime: formatUptime(stats.server.uptime_seconds),
        })}
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
          <RefreshCw
            className={[
              "-ml-0.5 mr-1 h-3.5 w-3.5",
              isFetching ? "animate-spin" : "",
            ].join(" ")}
          />
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
    <section className="flex flex-col gap-4">
      <SectionHeader
        icon={Activity}
        title={t("admin.systemHealth.title", { defaultValue: "Estado" })}
        subtitle={t("admin.systemHealth.subtitle", {
          defaultValue:
            "Comprobaciones rápidas de los componentes que tienen que estar arriba para que HubPlay funcione.",
        })}
      />
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
      <SeverityDot
        tone={ok ? "success" : "error"}
        label={ok ? "OK" : "FALLO"}
      />
      <span className="min-w-[110px] font-medium text-text-primary">
        {label}
      </span>
      <span className="text-text-secondary truncate flex-1 min-w-0 font-mono text-xs">
        {ok ? okText : errorText}
      </span>
      {detail && (
        <span className="basis-full pl-5 text-xs text-text-muted">
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
    <section className="flex flex-col gap-4">
      <SectionHeader
        icon={Network}
        title={t("admin.systemConnection.title", { defaultValue: "Conexión" })}
        subtitle={t("admin.systemConnection.hint", {
          defaultValue:
            "El servidor escucha en la dirección de abajo y se identifica externamente con la URL pública.",
        })}
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <ConnectionField
          label={t("admin.system.bindAddress")}
          value={stats.server.bind_address || "—"}
          hint={t("admin.systemConnection.bindHint", {
            defaultValue:
              "Configurada vía hubplay.yaml o $HUBPLAY_SERVER_HOST/PORT.",
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

function StreamingSection({
  stats,
  history,
}: {
  stats: SystemStats;
  history: MetricsSample[];
}) {
  const { t } = useTranslation();
  const { transcode_sessions_active, transcode_sessions_max } = stats.streaming;
  const max = transcode_sessions_max;
  const active = transcode_sessions_active;
  const pct = max > 0 ? Math.min(100, (active / max) * 100) : 0;

  const accelEnabled = stats.ffmpeg.hw_accel_enabled;
  const selected = stats.ffmpeg.hw_accel_selected;
  const available = stats.ffmpeg.hw_accels_available ?? [];
  let accelLabel: string;
  let accelTone: "success" | "warning" | "neutral";
  if (!accelEnabled) {
    accelLabel = t("admin.system.hwAccelDisabledLabel");
    accelTone = "neutral";
  } else if (!selected || selected === "none") {
    accelLabel = t("admin.system.hwAccelNone");
    accelTone = "warning";
  } else {
    accelLabel = selected.toUpperCase();
    accelTone = "success";
  }

  return (
    <section className="flex flex-col gap-4">
      <SectionHeader
        icon={Zap}
        title={t("admin.system.sectionStreaming")}
        subtitle={t("admin.systemStreaming.subtitle", {
          defaultValue:
            "Sesiones de transcodificación activas y backend de aceleración por hardware.",
        })}
      />
      <div className="grid gap-4 lg:grid-cols-2">
        {/* Sessions meter — sparkline charts the last hour of
            concurrent transcodes so the admin can spot peaks at a
            glance, not just the current value. */}
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
          <div className="flex items-end justify-between gap-3">
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
            <Sparkline
              values={history.map((h) => h.sessions)}
              width={120}
              height={32}
            />
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

        {/* HW accel */}
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
          <div className="flex items-center gap-3">
            <span className="text-2xl font-semibold text-text-primary">
              {accelLabel}
            </span>
            <SeverityDot
              tone={accelTone}
              label={
                accelEnabled
                  ? t("admin.system.hwAccelEnabled", { defaultValue: "Activado" })
                  : t("admin.system.hwAccelDisabledLabel")
              }
            />
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

      {/* Live "Now Playing" — uses the existing /admin/system/sessions
          5 s endpoint. Inline here so the admin sees what's actually
          consuming a slot, not just a number. */}
      <ActiveSessionsList />
    </section>
  );
}

// ─── Sesiones activas ──────────────────────────────────────────────

function ActiveSessionsList() {
  const { t } = useTranslation();
  const sessions = useAdminStreamSessions();
  const kill = useKillAdminStreamSession();
  const data = sessions.data ?? [];

  const handleKill = (s: AdminStreamSession) => {
    if (
      !window.confirm(
        t("admin.systemSessions.killConfirm", {
          defaultValue:
            "¿Cerrar la sesión de {{user}}? El cliente volverá a la pantalla anterior.",
          user: s.username || s.user_id,
        }),
      )
    ) {
      return;
    }
    kill.mutate({ sessionID: s.session_id });
  };

  if (data.length === 0) {
    return (
      <div className="rounded-[--radius-lg] border border-dashed border-border bg-bg-card px-5 py-6 text-center text-xs text-text-muted">
        {t("admin.systemSessions.empty", {
          defaultValue: "Nadie está reproduciendo nada ahora mismo.",
        })}
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-[--radius-lg] border border-border bg-bg-card">
      <div className="flex items-center justify-between border-b border-border-subtle px-5 py-2.5">
        <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
          {t("admin.systemSessions.title", {
            defaultValue: "Sesiones activas",
          })}
        </span>
        <span className="text-[10px] text-text-muted">
          {t("admin.systemSessions.refreshHint", {
            defaultValue: "Actualiza cada 5 s",
          })}
        </span>
      </div>
      <ul className="divide-y divide-border-subtle">
        {data.map((s) => (
          <li
            key={s.session_id}
            className="flex flex-wrap items-center gap-3 px-5 py-3 text-sm"
          >
            <span className="font-medium text-text-primary">
              {s.username || s.user_id}
            </span>
            <span className="text-text-muted">·</span>
            <span className="truncate text-text-secondary">
              {s.item_title || s.item_id}
            </span>
            <span className="ml-auto inline-flex items-center gap-2 text-xs text-text-muted">
              <span
                className={[
                  "rounded-full px-2 py-0.5 text-[10px] font-medium",
                  s.method === "Transcode"
                    ? "bg-warning/15 text-warning"
                    : "bg-success/15 text-success",
                ].join(" ")}
              >
                {s.method}
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => handleKill(s)}
                isLoading={kill.isPending}
                title={t("admin.systemSessions.kill", {
                  defaultValue: "Cerrar sesión",
                })}
              >
                <Square className="h-3.5 w-3.5" />
              </Button>
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// ─── Runtime (proceso) ─────────────────────────────────────────────

function RuntimeSection({
  runtime,
  history,
}: {
  runtime: SystemRuntimeStats;
  history: MetricsSample[];
}) {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-4">
      <SectionHeader
        icon={Cpu}
        title={t("admin.systemRuntime.title", {
          defaultValue: "Proceso",
        })}
        subtitle={t("admin.systemRuntime.subtitle", {
          defaultValue:
            "Memoria que el runtime de Go tiene reservada y goroutines vivas en este momento.",
        })}
      />
      <div className="grid gap-4 sm:grid-cols-3">
        <MetricCard
          label={t("admin.systemRuntime.memAlloc", {
            defaultValue: "Memoria asignada",
          })}
          value={`${runtime.memory_alloc_mb.toFixed(1)} MB`}
          sparkValues={history.map((h) => h.memMb)}
          hint={t("admin.systemRuntime.memAllocHint", {
            defaultValue: "Heap de Go en uso (no la RAM total del host).",
          })}
        />
        <MetricCard
          label={t("admin.systemRuntime.memSys", {
            defaultValue: "Memoria reservada",
          })}
          value={`${runtime.memory_sys_mb.toFixed(1)} MB`}
          hint={t("admin.systemRuntime.memSysHint", {
            defaultValue: "Lo que el runtime ha pedido al SO.",
          })}
        />
        <MetricCard
          label={t("admin.systemRuntime.goroutines", {
            defaultValue: "Goroutines",
          })}
          value={String(runtime.goroutines)}
          hint={`${runtime.cpu_count} CPU · ${runtime.os}/${runtime.arch}`}
        />
      </div>
    </section>
  );
}

function MetricCard({
  label,
  value,
  sparkValues,
  hint,
}: {
  label: string;
  value: string;
  sparkValues?: number[];
  hint: string;
}) {
  return (
    <div className="flex flex-col gap-2 rounded-[--radius-lg] border border-border bg-bg-card p-5">
      <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
        {label}
      </span>
      <div className="flex items-end justify-between gap-3">
        <span className="text-2xl font-semibold text-text-primary tabular-nums">
          {value}
        </span>
        {sparkValues && (
          <Sparkline values={sparkValues} width={100} height={28} />
        )}
      </div>
      <p className="text-[11px] text-text-muted leading-relaxed">{hint}</p>
    </div>
  );
}

// ─── Almacenamiento ─────────────────────────────────────────────────

function StorageSection({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const s = stats.storage;
  const dbBytes = stats.database.size_bytes;
  const total = (s.image_dir_bytes ?? 0) + (s.transcode_cache_bytes ?? 0) + dbBytes;
  // Bars share the same denominator (the largest of the three) so
  // the eye registers relative size at a glance.
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
    <section className="flex flex-col gap-4">
      <SectionHeader
        icon={HardDrive}
        title={t("admin.system.sectionStorage")}
        subtitle={t("admin.systemStorage.subtitle", {
          defaultValue:
            "Espacio que ocupan los caches y la base de datos en disco.",
        })}
        trailing={
          <span className="text-sm text-text-muted tabular-nums">
            {t("admin.systemStorage.total", {
              defaultValue: "Total {{size}}",
              size: formatBytes(total),
            })}
          </span>
        }
      />
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

// ─── Configuración ─────────────────────────────────────────────────

function SettingsSection() {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-4">
      <SectionHeader
        icon={Settings2}
        title={t("admin.systemSettings.title", { defaultValue: "Configuración" })}
        subtitle={t("admin.systemSettings.subtitle", {
          defaultValue:
            "Valores en tiempo de ejecución que se aplican sin reiniciar el servidor.",
        })}
      />
      <SystemSettingsSection />
    </section>
  );
}

// ─── Avanzado ──────────────────────────────────────────────────────

function AdvancedSection() {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-4 pt-6 border-t border-border-subtle">
      <SectionHeader
        icon={KeyRound}
        title={t("admin.system.sectionAdvanced")}
        subtitle={t("admin.systemAdvanced.subtitle", {
          defaultValue:
            "Operaciones destructivas y rotación de llaves. Nada que tocar en el día a día.",
        })}
      />
      <div
        role="note"
        className="rounded-[--radius-md] border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-warning"
      >
        {t("admin.advanced.warning")}
      </div>
      <BackupPanel />
      <AuthKeysPanel />
    </section>
  );
}

