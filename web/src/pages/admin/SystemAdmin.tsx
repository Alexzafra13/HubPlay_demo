import { useSystemStats } from "@/api/hooks";
import type { SystemStats } from "@/api/types";
import { Badge, Spinner, Button, EmptyState } from "@/components/common";
import { useTranslation } from "react-i18next";
import { AuthKeysPanel } from "@/components/admin/AuthKeysPanel";

// Refresh cadence for the live stats. 30s matches the old behaviour and is
// frequent enough to feel live without flooding the dir-walk on the
// backend (image cache + transcode cache).
const REFETCH_MS = 30_000;

// formatUptime turns seconds into a Plex-style "12d 3h 7m" string.
// Days/hours collapse silently when zero so short uptimes don't show "0d".
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

// formatBytes — short human-readable size. We never need fractional KiB so
// the rounding rule is "drop decimals below MiB, one decimal from MiB up".
// Returns "—" for zero so missing/empty caches read clearly.
function formatBytes(n: number): string {
  if (!n || n <= 0) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i <= 1 ? `${Math.round(v)} ${units[i]}` : `${v.toFixed(1)} ${units[i]}`;
}

interface StatCardProps {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
}

function StatCard({ label, value, hint }: StatCardProps) {
  return (
    <div className="flex flex-col gap-2 rounded-[--radius-lg] bg-bg-card border border-border p-5">
      <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
        {label}
      </span>
      <span className="text-lg font-semibold text-text-primary break-all">
        {value}
      </span>
      {hint && (
        <span className="text-xs text-text-muted break-all">{hint}</span>
      )}
    </div>
  );
}

interface SectionProps {
  title: string;
  children: React.ReactNode;
}

function Section({ title, children }: SectionProps) {
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
        {title}
      </h3>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {children}
      </div>
    </section>
  );
}

// SystemAdmin — "what's going on with my server" at a glance. Mirrors what
// Plex puts in Status > Dashboard: live status badges, uptime, FFmpeg
// detection, hardware acceleration, transcode load, runtime memory, and
// on-disk cache footprints. The signing-key panel lives at the bottom
// since it's a destructive admin action that should not be the first
// thing the eye lands on.
export default function SystemAdmin() {
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
    <div className="flex flex-col gap-8">
      <SystemHeader
        stats={stats}
        dataUpdatedAt={dataUpdatedAt}
        isFetching={isFetching}
        onRefresh={() => refetch()}
      />

      <Section title={t("admin.system.sectionServer")}>
        <ServerCards stats={stats} />
      </Section>

      <Section title={t("admin.system.sectionStreaming")}>
        <StreamingCards stats={stats} />
      </Section>

      <Section title={t("admin.system.sectionRuntime")}>
        <RuntimeCards stats={stats} />
      </Section>

      <Section title={t("admin.system.sectionStorage")}>
        <StorageCards stats={stats} />
      </Section>

      <AuthKeysPanel />
    </div>
  );
}

interface HeaderProps {
  stats: SystemStats;
  dataUpdatedAt: number;
  isFetching: boolean;
  onRefresh: () => void;
}

function SystemHeader({ stats, dataUpdatedAt, isFetching, onRefresh }: HeaderProps) {
  const { t } = useTranslation();

  // Aggregate "is everything green?" — the badge in the page header is a
  // single traffic light. DB ping is the most failure-prone of the three;
  // FFmpeg missing is fatal for transcode but not for direct play, so we
  // weight it equally with DB to surface the symptom early.
  const dbOk = stats.database.ok;
  const ffmpegOk = stats.ffmpeg.found;
  const allHealthy = dbOk && ffmpegOk;

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-semibold text-text-primary">
          {t("admin.system.title")}
        </h2>
        <Badge variant={allHealthy ? "success" : "error"}>
          {allHealthy ? t("admin.system.healthy") : t("admin.system.degraded")}
        </Badge>
      </div>

      <div className="flex items-center gap-3 text-xs text-text-muted">
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
      </div>
    </div>
  );
}

function ServerCards({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const dbOk = stats.database.ok;
  const ffmpegOk = stats.ffmpeg.found;

  return (
    <>
      <StatCard
        label={t("admin.system.version")}
        value={stats.server.version || "—"}
        hint={stats.server.go_version}
      />
      <StatCard
        label={t("admin.system.uptime")}
        value={<span className="tabular-nums">{formatUptime(stats.server.uptime_seconds)}</span>}
      />
      <StatCard
        label={t("admin.system.database")}
        value={
          <Badge variant={dbOk ? "success" : "error"}>
            {dbOk ? t("admin.system.healthy") : t("admin.system.degraded")}
          </Badge>
        }
        hint={stats.database.error || formatBytes(stats.database.size_bytes)}
      />
      <StatCard
        label={t("admin.system.ffmpeg")}
        value={
          <Badge variant={ffmpegOk ? "success" : "error"}>
            {ffmpegOk ? t("admin.system.ffmpegFound") : t("admin.system.ffmpegMissing")}
          </Badge>
        }
        hint={stats.ffmpeg.path || undefined}
      />
    </>
  );
}

function StreamingCards({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();

  const max = stats.streaming.transcode_sessions_max;
  const active = stats.streaming.transcode_sessions_active;
  const transcodeHint =
    max > 0
      ? t("admin.system.transcodeSlots", { active, max })
      : t("admin.system.transcodeUnlimited", { active });

  // hw_accel_selected is the canonical wire string ("vaapi", "nvenc", "none").
  // We translate "none" to a friendlier label; everything else is an
  // identifier admins recognise, so we render it as-is in uppercase.
  const selected = stats.ffmpeg.hw_accel_selected;
  const accelLabel =
    !selected || selected === "none"
      ? t("admin.system.hwAccelNone")
      : selected.toUpperCase();
  const availableLabel =
    stats.ffmpeg.hw_accels_available.length > 0
      ? stats.ffmpeg.hw_accels_available.map((a) => a.toUpperCase()).join(", ")
      : "—";

  return (
    <>
      <StatCard
        label={t("admin.system.activeTranscodes")}
        value={<span className="tabular-nums">{active}</span>}
        hint={transcodeHint}
      />
      <StatCard
        label={t("admin.system.hwAccelSelected")}
        value={accelLabel}
        hint={
          stats.ffmpeg.hw_accel_encoder && selected !== "none"
            ? `${t("admin.system.hwAccelEncoder")}: ${stats.ffmpeg.hw_accel_encoder}`
            : undefined
        }
      />
      <StatCard
        label={t("admin.system.hwAccelAvailable")}
        value={<span className="text-sm font-medium">{availableLabel}</span>}
      />
    </>
  );
}

function RuntimeCards({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const r = stats.runtime;
  return (
    <>
      <StatCard
        label={t("admin.system.memoryAlloc")}
        value={<span className="tabular-nums">{r.memory_alloc_mb} MiB</span>}
        hint={`${t("admin.system.memorySys")}: ${r.memory_sys_mb} MiB`}
      />
      <StatCard
        label={t("admin.system.goroutines")}
        value={<span className="tabular-nums">{r.goroutines}</span>}
        hint={`${t("admin.system.gcPause")}: ${r.gc_pause_ms} ms`}
      />
      <StatCard
        label={t("admin.system.cpuCount")}
        value={<span className="tabular-nums">{r.cpu_count}</span>}
        hint={`${t("admin.system.platform")}: ${r.os}/${r.arch}`}
      />
    </>
  );
}

function StorageCards({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const s = stats.storage;
  return (
    <>
      <StatCard
        label={t("admin.system.imageDir")}
        value={<span className="tabular-nums">{formatBytes(s.image_dir_bytes)}</span>}
        hint={s.image_dir_path}
      />
      <StatCard
        label={t("admin.system.transcodeCache")}
        value={<span className="tabular-nums">{formatBytes(s.transcode_cache_bytes)}</span>}
        hint={s.transcode_cache_path}
      />
      <StatCard
        label={t("admin.system.databaseSize")}
        value={<span className="tabular-nums">{formatBytes(stats.database.size_bytes)}</span>}
        hint={stats.database.path}
      />
    </>
  );
}
