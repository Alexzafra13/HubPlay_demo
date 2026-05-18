import { useEffect, useRef, useState } from "react";
import {
  Activity,
  Box,
  Cpu,
  Database,
  HardDrive,
  KeyRound,
  MemoryStick,
  MonitorPlay,
  Network,
  RefreshCw,
  Server,
  Settings2,
  Square,
  Trophy,
  Users,
  Zap,
} from "lucide-react";
import {
  useAdminStreamActivity,
  useAdminStreamSessions,
  useAdminTopItems,
  useKillAdminStreamSession,
  useSystemStats,
} from "@/api/hooks";
import { usePeers } from "@/api/hooks/federation";
import type {
  AdminStreamSession,
  AdminTopItem,
  SystemStats,
} from "@/api/types";
import { Button, EmptyState, Spinner } from "@/components/common";
import { AuthKeysPanel } from "@/components/admin/AuthKeysPanel";
import { BackupPanel } from "@/components/admin/BackupPanel";
import { DatabasePanel } from "@/components/admin/DatabasePanel";
import { LogsPanel } from "@/components/admin/LogsPanel";
import { SectionHeader } from "@/components/admin/SectionHeader";
import { AreaTimeline } from "@/components/admin/dashboard/AreaTimeline";
import { BarTimeline } from "@/components/admin/dashboard/BarTimeline";
import { ChartCard } from "@/components/admin/dashboard/ChartCard";
import {
  HealthPill,
  type HealthTone,
} from "@/components/admin/dashboard/HealthPill";
import { KpiTile } from "@/components/admin/dashboard/KpiTile";
import { useTranslation } from "react-i18next";

import { SystemSettingsSection } from "./SystemSettingsSection";

// SystemStatus — admin "Sistema" page, rediseño bento.
//
// Layout top-to-bottom:
//
//   1. IdentityStrip      — quien soy + version + uptime + hardware
//   2. HealthStrip        — pills horizontales: DB · FFmpeg · Federation · URL
//   3. KpiRow             — 5 tiles uniformes: CPU · RAM · GPU · Sessions · Bibliotecas
//   4. HostChartsRow      — 2 area charts (CPU 1h + RAM 1h) lado a lado
//   5. ActiveSessionsList — tabla full-width (queda como estaba)
//   6. ActivityRow        — bar chart de minutos vistos 14d + lista top items 7d
//   7. InfraRow           — 3 cards: Storage, Database, Connection
//   8. RuntimeSection     — 3 tiles del proceso Go (compacto)
//   9. SettingsSection    — editor runtime
//  10. AdvancedSection    — collapsibles: logs, backup, db, auth keys
//
// El refetch de stats es 30 s; el ring buffer cliente captura un
// sample por refetch -> ~120 samples en 1 h. Para historicos largos
// (24h/7d) el operador puede activar /metrics + Grafana externo
// (hubplay.yaml -> observability.metrics_enabled, default true).

const REFETCH_MS = 30_000;

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
  const units = ["B", "KB", "MB", "GB", "TB"];
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
  ts: number;
  sessions: number;
  memMb: number;
  cpuPercent: number;
  ramPercent: number;
}

const MAX_SAMPLES = 120; // ≈ 1 h at 30 s cadence

function useMetricsHistory(
  stats: SystemStats | undefined,
  dataUpdatedAt: number,
): MetricsSample[] {
  const [samples, setSamples] = useState<MetricsSample[]>([]);
  const lastTsRef = useRef(0);

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (!stats || dataUpdatedAt === 0 || dataUpdatedAt === lastTsRef.current) {
      return;
    }
    lastTsRef.current = dataUpdatedAt;
    setSamples((prev) => {
      const ramTotal = stats.host?.ram_total_bytes ?? 0;
      const ramUsed = stats.host?.ram_used_bytes ?? 0;
      const next = prev.concat({
        ts: dataUpdatedAt,
        sessions: stats.streaming.transcode_sessions_active,
        memMb: stats.runtime.memory_alloc_mb,
        cpuPercent: stats.host?.cpu_percent ?? 0,
        ramPercent: ramTotal > 0 ? Math.min(100, (ramUsed / ramTotal) * 100) : 0,
      });
      return next.length > MAX_SAMPLES ? next.slice(-MAX_SAMPLES) : next;
    });
  }, [stats, dataUpdatedAt]);
  /* eslint-enable react-hooks/set-state-in-effect */

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
    <div className="flex flex-col gap-6">
      <IdentityStrip
        stats={stats}
        dataUpdatedAt={dataUpdatedAt}
        isFetching={isFetching}
        onRefresh={() => refetch()}
      />

      <HealthStrip stats={stats} />

      <KpiRow stats={stats} history={history} />

      <HostChartsRow history={history} />

      <ActiveSessionsList />

      <ActivityRow />

      <InfraRow stats={stats} />

      <RuntimeRow stats={stats} history={history} />

      <SettingsSection />

      <AdvancedSection />
    </div>
  );
}

// ─── Identity strip ────────────────────────────────────────────────

function IdentityStrip({
  stats,
  dataUpdatedAt,
  isFetching,
  onRefresh,
}: {
  stats: SystemStats;
  dataUpdatedAt: number;
  isFetching: boolean;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();
  const allHealthy = stats.database.ok && stats.ffmpeg.found;
  const hostBits: string[] = [];
  if (stats.host?.cpu_model) {
    const clean = stats.host.cpu_model
      .replace(/\s+\d+-Core Processor.*$/i, "")
      .replace(/\(R\)|\(TM\)|CPU @ .*$/g, "")
      .trim();
    hostBits.push(clean);
  }
  if (stats.host?.cpu_cores_logical) {
    const p = stats.host.cpu_cores_physical;
    const l = stats.host.cpu_cores_logical;
    hostBits.push(p > 0 && p !== l ? `${p}c/${l}t` : `${l}c`);
  }
  if (stats.ffmpeg.hw_accel_selected && stats.ffmpeg.hw_accel_selected !== "none") {
    hostBits.push(stats.ffmpeg.hw_accel_selected.toUpperCase());
  }
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
      {hostBits.length > 0 && (
        <>
          <span className="text-text-muted">·</span>
          <span
            className="text-text-secondary truncate max-w-[40ch]"
            title={stats.host?.cpu_model}
          >
            {hostBits.join(" · ")}
          </span>
        </>
      )}
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

// ─── Health strip ───────────────────────────────────────────────────
//
// Cuatro pills horizontales con el estado de los subsistemas que
// pueden romperse. Sustituye al HealthSection antiguo (lista de 3
// filas full-width que ocupaba muchisimo para 3 booleans).

function HealthStrip({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const peers = usePeers();
  const pairedCount = (peers.data ?? []).filter(
    (p) => p.status === "paired",
  ).length;
  const peersTone: HealthTone =
    peers.isLoading
      ? "neutral"
      : peers.error
        ? "warning"
        : pairedCount > 0
          ? "success"
          : "neutral";

  return (
    <section
      className="flex flex-wrap items-center gap-2"
      aria-label={t("admin.systemHealth.title", { defaultValue: "Estado" })}
    >
      <HealthPill
        label={t("admin.system.database", { defaultValue: "Base de datos" })}
        tone={stats.database.ok ? "success" : "error"}
        detail={
          stats.database.ok
            ? `${stats.database.path} · ${formatBytes(stats.database.size_bytes)}`
            : (stats.database.error ?? t("admin.system.degraded"))
        }
      />
      <HealthPill
        label="FFmpeg"
        tone={stats.ffmpeg.found ? "success" : "error"}
        detail={
          stats.ffmpeg.found
            ? `${stats.ffmpeg.path}`
            : t("admin.system.ffmpegMissing")
        }
      />
      <HealthPill
        label={t("admin.federation.title", { defaultValue: "Federation" })}
        tone={peersTone}
        detail={
          peers.isLoading
            ? "…"
            : peers.error
              ? String(peers.error)
              : pairedCount === 0
                ? t("admin.systemHealth.federationNoPeers", {
                    defaultValue: "Sin peers emparejados",
                  })
                : t("admin.systemHealth.federationPaired", {
                    defaultValue: "{{n}} peers emparejados",
                    n: pairedCount,
                  })
        }
        trailing={pairedCount > 0 ? String(pairedCount) : undefined}
      />
      <HealthPill
        label={t("admin.system.baseURL", { defaultValue: "URL pública" })}
        tone={stats.server.base_url ? "success" : "warning"}
        detail={stats.server.base_url || t("admin.system.baseURLUnset")}
      />
    </section>
  );
}

// ─── KPI row ────────────────────────────────────────────────────────
//
// 5 tiles uniformes. El user pidio explicitamente "muchos
// contenedores cogen todo el ancho y se quedan muy grande para
// lo que ofrecen" — los KPIs son la respuesta directa.

function KpiRow({
  stats,
  history,
}: {
  stats: SystemStats;
  history: MetricsSample[];
}) {
  const { t } = useTranslation();
  const cpuPct = stats.host?.cpu_percent ?? 0;
  const ramTotal = stats.host?.ram_total_bytes ?? 0;
  const ramUsed = stats.host?.ram_used_bytes ?? 0;
  const ramRatio = ramTotal > 0 ? ramUsed / ramTotal : 0;
  const sessionsMax = stats.streaming.transcode_sessions_max;
  const sessionsActive = stats.streaming.transcode_sessions_active;
  const sessionsRatio =
    sessionsMax > 0 ? Math.min(1, sessionsActive / sessionsMax) : undefined;

  // Storage: usamos image + transcode + DB como aproximacion a "lo
  // que HubPlay ocupa". Si quisieramos disk-total-vs-free habria
  // que añadirlo en /admin/system/stats (futuro).
  const storageUsed =
    (stats.storage?.image_dir_bytes ?? 0) +
    (stats.storage?.transcode_cache_bytes ?? 0) +
    stats.database.size_bytes;

  const itemsTotal = stats.libraries?.items_total ?? 0;
  const librariesCount = stats.libraries?.total ?? 0;

  return (
    <section
      aria-label={t("admin.systemKpi.title", { defaultValue: "Resumen" })}
      className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5"
    >
      <KpiTile
        label={t("admin.systemHost.cpu", { defaultValue: "CPU" })}
        icon={Cpu}
        value={cpuPct.toFixed(1)}
        unit="%"
        ratio={cpuPct / 100}
        sparkline={history.map((h) => h.cpuPercent)}
      />
      <KpiTile
        label={t("admin.systemHost.ram", { defaultValue: "RAM" })}
        icon={MemoryStick}
        value={formatBytes(ramUsed)}
        unit={`/ ${formatBytes(ramTotal)}`}
        ratio={ramRatio}
        sparkline={history.map((h) => h.ramPercent)}
      />
      <KpiTile
        label={t("admin.systemHost.gpu", { defaultValue: "GPU" })}
        icon={Zap}
        value={
          stats.ffmpeg.hw_accel_selected &&
          stats.ffmpeg.hw_accel_selected !== "none"
            ? stats.ffmpeg.hw_accel_selected.toUpperCase()
            : "—"
        }
        hint={
          stats.host?.gpu_model
            ? stats.host.gpu_model
            : stats.ffmpeg.hw_accel_enabled
              ? t("admin.systemHost.gpuSoftware", {
                  defaultValue: "Sin GPU dedicada",
                })
              : t("admin.system.hwAccelDisabledLabel")
        }
        tone={
          stats.ffmpeg.hw_accel_enabled &&
          stats.ffmpeg.hw_accel_selected &&
          stats.ffmpeg.hw_accel_selected !== "none"
            ? "success"
            : "neutral"
        }
      />
      <KpiTile
        label={t("admin.systemKpi.sessions", { defaultValue: "Sesiones" })}
        icon={MonitorPlay}
        value={sessionsActive}
        unit={sessionsMax > 0 ? `/ ${sessionsMax}` : undefined}
        ratio={sessionsRatio}
        sparkline={history.map((h) => h.sessions)}
        hint={
          sessionsMax > 0
            ? t("admin.systemKpi.sessionsHint", {
                defaultValue: "{{a}} activas · {{m}} max",
                a: sessionsActive,
                m: sessionsMax,
              })
            : t("admin.systemKpi.sessionsUnlimitedHint", {
                defaultValue: "Sin límite configurado",
              })
        }
      />
      <KpiTile
        label={t("admin.systemKpi.library", { defaultValue: "Biblioteca" })}
        icon={Box}
        value={itemsTotal.toLocaleString()}
        unit={t("admin.systemKpi.items", { defaultValue: "items" })}
        hint={
          librariesCount > 0
            ? t("admin.systemKpi.libraries", {
                defaultValue: "{{n}} bibliotecas · {{size}} en disco",
                n: librariesCount,
                size: formatBytes(storageUsed),
              })
            : t("admin.systemKpi.libraryEmpty", {
                defaultValue: "Sin bibliotecas configuradas",
              })
        }
        tone="neutral"
      />
    </section>
  );
}

// ─── Host charts row (CPU + RAM) ────────────────────────────────────
//
// Dos area charts grandes lado a lado para que el operador vea
// tendencia de la ultima hora de un vistazo. Empty state hasta
// que el ring buffer junta 2+ samples.

function HostChartsRow({ history }: { history: MetricsSample[] }) {
  const { t } = useTranslation();
  const empty = history.length < 2;
  // Recharts necesita data plana - mapeamos a {label, value}.
  const cpuData = history.map((h) => ({
    label: new Date(h.ts).toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    }),
    value: h.cpuPercent,
  }));
  const ramData = history.map((h) => ({
    label: new Date(h.ts).toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    }),
    value: h.ramPercent,
  }));
  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <ChartCard
        icon={Cpu}
        title={t("admin.systemCharts.cpuTitle", {
          defaultValue: "CPU últimos minutos",
        })}
        subtitle={t("admin.systemCharts.cpuSubtitle", {
          defaultValue: "Carga del host, ventana de ~1 h en el cliente.",
        })}
        empty={empty}
        emptyText={t("admin.systemCharts.collecting", {
          defaultValue: "Recogiendo datos…",
        })}
        height={180}
      >
        <AreaTimeline
          data={cpuData}
          xKey="label"
          yKey="value"
          color="var(--color-accent)"
          unit="%"
          yDomain={[0, 100]}
          formatY={(v) => `${v.toFixed(1)}%`}
        />
      </ChartCard>
      <ChartCard
        icon={MemoryStick}
        title={t("admin.systemCharts.ramTitle", {
          defaultValue: "RAM últimos minutos",
        })}
        subtitle={t("admin.systemCharts.ramSubtitle", {
          defaultValue: "% del total. Sube con scans y transcodes activos.",
        })}
        empty={empty}
        emptyText={t("admin.systemCharts.collecting", {
          defaultValue: "Recogiendo datos…",
        })}
        height={180}
      >
        <AreaTimeline
          data={ramData}
          xKey="label"
          yKey="value"
          color="var(--color-success)"
          unit="%"
          yDomain={[0, 100]}
          formatY={(v) => `${v.toFixed(1)}%`}
        />
      </ChartCard>
    </div>
  );
}

// ─── Active sessions list ──────────────────────────────────────────

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

  return (
    <section className="flex flex-col gap-3">
      <SectionHeader
        icon={MonitorPlay}
        title={t("admin.systemSessions.title", {
          defaultValue: "Sesiones activas",
        })}
        subtitle={t("admin.systemSessions.subtitle", {
          defaultValue:
            "Quién está reproduciendo qué ahora mismo. Refresca cada 5 s.",
        })}
        trailing={
          <span className="rounded-full border border-border-subtle bg-bg-elevated px-2 py-0.5 text-[10px] font-medium tabular-nums text-text-secondary">
            {data.length}
          </span>
        }
      />
      {data.length === 0 ? (
        <div className="rounded-[--radius-lg] border border-dashed border-border bg-bg-card px-5 py-6 text-center text-xs text-text-muted">
          {t("admin.systemSessions.empty", {
            defaultValue: "Nadie está reproduciendo nada ahora mismo.",
          })}
        </div>
      ) : (
        <div className="overflow-hidden rounded-[--radius-lg] border border-border bg-bg-card">
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
      )}
    </section>
  );
}

// ─── Activity row (watch days + top items) ──────────────────────────

function ActivityRow() {
  const { t } = useTranslation();
  const activity = useAdminStreamActivity(14);
  const top = useAdminTopItems(7, 5);

  const buckets = activity.data?.buckets ?? [];
  const totalMinutes = buckets.reduce((s, b) => s + b.watch_minutes, 0);
  const activityData = buckets.map((b) => ({
    date: b.date,
    minutes: b.watch_minutes,
  }));
  const topItems = top.data?.items ?? [];

  return (
    <div className="grid gap-3 lg:grid-cols-[3fr_2fr]">
      <ChartCard
        icon={Activity}
        title={t("admin.systemActivity.title", {
          defaultValue: "Actividad de visionado",
        })}
        subtitle={t("admin.systemActivity.subtitle", {
          defaultValue: "Minutos totales por día (últimos 14 días).",
        })}
        trailing={
          <span className="text-[11px] text-text-muted tabular-nums">
            {t("admin.systemActivity.total", {
              defaultValue: "{{n}} min totales",
              n: totalMinutes.toLocaleString(),
            })}
          </span>
        }
        loading={activity.isLoading}
        empty={!activity.isLoading && totalMinutes === 0}
        emptyText={t("admin.systemActivity.empty", {
          defaultValue: "Nadie ha reproducido nada en estos 14 días.",
        })}
        height={200}
      >
        <BarTimeline
          data={activityData}
          xKey="date"
          yKey="minutes"
          color="var(--color-accent)"
          unit=" min"
          formatX={(v) => {
            const d = new Date(String(v));
            return d.toLocaleDateString(undefined, {
              day: "2-digit",
              month: "2-digit",
            });
          }}
          formatY={(n) => `${n} min`}
        />
      </ChartCard>
      <ChartCard
        icon={Trophy}
        title={t("admin.systemTopItems.title", {
          defaultValue: "Top reproducciones",
        })}
        subtitle={t("admin.systemTopItems.subtitle", {
          defaultValue: "Lo más visto en los últimos 7 días.",
        })}
        loading={top.isLoading}
        empty={!top.isLoading && topItems.length === 0}
        emptyText={t("admin.systemTopItems.empty", {
          defaultValue: "Sin reproducciones en la última semana.",
        })}
        height={200}
      >
        <TopItemsList items={topItems} />
      </ChartCard>
    </div>
  );
}

function TopItemsList({ items }: { items: AdminTopItem[] }) {
  const max = items.reduce((m, x) => Math.max(m, x.play_count), 1);
  return (
    <ol className="flex h-full flex-col justify-around gap-1.5 px-1">
      {items.map((it, idx) => {
        const pct = (it.play_count / max) * 100;
        return (
          <li key={it.id} className="flex flex-col gap-1">
            <div className="flex items-baseline justify-between gap-2 text-xs">
              <span className="flex min-w-0 items-baseline gap-1.5">
                <span className="tabular-nums text-text-muted/70">
                  {idx + 1}
                </span>
                <span className="truncate font-medium text-text-primary">
                  {it.title}
                </span>
              </span>
              <span className="tabular-nums text-text-secondary">
                {it.play_count}
              </span>
            </div>
            <div className="h-1 w-full overflow-hidden rounded-full bg-bg-elevated">
              <div
                className="h-full bg-accent/70 transition-all"
                style={{ width: `${pct}%` }}
              />
            </div>
          </li>
        );
      })}
    </ol>
  );
}

// ─── Infra row (Storage + Database + Connection) ───────────────────

function InfraRow({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const s = stats.storage;
  const dbBytes = stats.database.size_bytes;
  const imageBytes = s?.image_dir_bytes ?? 0;
  const transcodeBytes = s?.transcode_cache_bytes ?? 0;
  const total = imageBytes + transcodeBytes + dbBytes;
  const denom = Math.max(imageBytes, transcodeBytes, dbBytes, 1);
  return (
    <div className="grid gap-3 lg:grid-cols-3">
      <InfraCard
        icon={HardDrive}
        title={t("admin.system.sectionStorage", {
          defaultValue: "Almacenamiento",
        })}
        trailing={formatBytes(total)}
      >
        <StorageBars
          rows={[
            {
              label: t("admin.system.imageDir", {
                defaultValue: "Imágenes",
              }),
              bytes: imageBytes,
              path: s?.image_dir_path,
            },
            {
              label: t("admin.system.transcodeCache", {
                defaultValue: "Caché de transcodificación",
              }),
              bytes: transcodeBytes,
              path: s?.transcode_cache_path,
            },
            {
              label: t("admin.system.databaseSize", {
                defaultValue: "Base de datos",
              }),
              bytes: dbBytes,
              path: stats.database.path,
            },
          ]}
          denom={denom}
        />
      </InfraCard>
      <InfraCard
        icon={Database}
        title={t("admin.systemInfra.databaseTitle", {
          defaultValue: "Base de datos",
        })}
      >
        <dl className="flex flex-col gap-2 text-xs">
          <InfraRowKV
            label={t("admin.systemInfra.dbStatus", { defaultValue: "Estado" })}
            value={
              <HealthPill
                label={stats.database.ok ? "OK" : "FALLO"}
                tone={stats.database.ok ? "success" : "error"}
              />
            }
          />
          <InfraRowKV
            label={t("admin.systemInfra.dbSize", { defaultValue: "Tamaño" })}
            value={formatBytes(dbBytes)}
            mono
          />
          <InfraRowKV
            label={t("admin.systemInfra.dbPath", { defaultValue: "Ruta" })}
            value={stats.database.path}
            mono
            truncate
          />
        </dl>
      </InfraCard>
      <InfraCard
        icon={Network}
        title={t("admin.systemInfra.networkTitle", {
          defaultValue: "Conexión",
        })}
      >
        <dl className="flex flex-col gap-2 text-xs">
          <InfraRowKV
            label={t("admin.system.bindAddress", {
              defaultValue: "Bind address",
            })}
            value={stats.server.bind_address || "—"}
            mono
          />
          <InfraRowKV
            label={t("admin.system.baseURL", {
              defaultValue: "URL pública",
            })}
            value={stats.server.base_url || "—"}
            mono
            truncate
          />
          <InfraRowKV
            label="FFmpeg"
            value={stats.ffmpeg.found ? stats.ffmpeg.path : "—"}
            mono
            truncate
          />
        </dl>
      </InfraCard>
    </div>
  );
}

function InfraCard({
  icon: Icon,
  title,
  trailing,
  children,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  trailing?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex h-full flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-4">
      <header className="flex items-baseline justify-between gap-3">
        <div className="flex items-center gap-2 text-text-muted">
          <Icon className="h-3.5 w-3.5" />
          <span className="text-[10px] font-medium uppercase tracking-wider">
            {title}
          </span>
        </div>
        {trailing && (
          <span className="text-xs tabular-nums text-text-secondary">
            {trailing}
          </span>
        )}
      </header>
      {children}
    </div>
  );
}

function InfraRowKV({
  label,
  value,
  mono,
  truncate,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
  truncate?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-3 min-w-0">
      <dt className="flex-none text-text-muted">{label}</dt>
      <dd
        className={[
          "min-w-0 text-text-secondary text-right",
          mono ? "font-mono" : "",
          truncate ? "truncate" : "break-all",
        ].join(" ")}
      >
        {value}
      </dd>
    </div>
  );
}

function StorageBars({
  rows,
  denom,
}: {
  rows: { label: string; bytes: number; path?: string }[];
  denom: number;
}) {
  return (
    <ul className="flex flex-col gap-2.5 text-xs">
      {rows.map((r) => {
        const pct = denom > 0 ? (r.bytes / denom) * 100 : 0;
        return (
          <li key={r.label} className="flex flex-col gap-1">
            <div className="flex items-baseline justify-between gap-2">
              <span className="font-medium text-text-primary truncate">
                {r.label}
              </span>
              <span className="tabular-nums text-text-secondary">
                {formatBytes(r.bytes)}
              </span>
            </div>
            <div className="h-1 w-full overflow-hidden rounded-full bg-bg-elevated">
              <div
                className="h-full bg-accent transition-all"
                style={{ width: `${pct}%` }}
              />
            </div>
            {r.path && (
              <span
                className="font-mono text-[10px] text-text-muted truncate"
                title={r.path}
              >
                {r.path}
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}

// ─── Runtime row (Go process internals) ────────────────────────────

function RuntimeRow({
  stats,
  history,
}: {
  stats: SystemStats;
  history: MetricsSample[];
}) {
  const { t } = useTranslation();
  const r = stats.runtime;
  return (
    <section className="flex flex-col gap-3">
      <SectionHeader
        icon={Server}
        title={t("admin.systemRuntime.title", { defaultValue: "Proceso" })}
        subtitle={t("admin.systemRuntime.subtitle", {
          defaultValue:
            "Internals del runtime de Go. Diferente de la RAM total del host de arriba.",
        })}
      />
      <div className="grid gap-3 sm:grid-cols-3">
        <KpiTile
          label={t("admin.systemRuntime.memAlloc", {
            defaultValue: "Heap usado",
          })}
          icon={MemoryStick}
          value={r.memory_alloc_mb.toFixed(1)}
          unit="MB"
          sparkline={history.map((h) => h.memMb)}
          hint={t("admin.systemRuntime.memAllocHint", {
            defaultValue: "Heap de Go activo, no la RAM del host.",
          })}
          tone="neutral"
        />
        <KpiTile
          label={t("admin.systemRuntime.memSys", {
            defaultValue: "Reservado al SO",
          })}
          icon={MemoryStick}
          value={r.memory_sys_mb.toFixed(1)}
          unit="MB"
          hint={t("admin.systemRuntime.memSysHint", {
            defaultValue: "Memoria que el runtime ha pedido al sistema.",
          })}
          tone="neutral"
        />
        <KpiTile
          label={t("admin.systemRuntime.goroutines", {
            defaultValue: "Goroutines",
          })}
          icon={Users}
          value={r.goroutines.toLocaleString()}
          hint={`${r.cpu_count} CPU · ${r.os}/${r.arch}`}
          tone="neutral"
        />
      </div>
    </section>
  );
}

// ─── Settings ───────────────────────────────────────────────────────

function SettingsSection() {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-3 pt-3 border-t border-border-subtle">
      <SectionHeader
        icon={Settings2}
        title={t("admin.systemSettings.title", {
          defaultValue: "Configuración",
        })}
        subtitle={t("admin.systemSettings.subtitle", {
          defaultValue:
            "Valores en tiempo de ejecución que se aplican sin reiniciar el servidor.",
        })}
      />
      <SystemSettingsSection />
    </section>
  );
}

// ─── Advanced ──────────────────────────────────────────────────────

function AdvancedSection() {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-3 pt-3 border-t border-border-subtle">
      <SectionHeader
        icon={KeyRound}
        title={t("admin.system.sectionAdvanced", {
          defaultValue: "Avanzado",
        })}
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
      <LogsPanel />
      <BackupPanel />
      <DatabasePanel />
      <AuthKeysPanel />
    </section>
  );
}
