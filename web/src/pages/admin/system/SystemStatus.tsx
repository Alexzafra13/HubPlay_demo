import { useSystemStats } from "@/api/hooks";
import type { SystemStats } from "@/api/types";
import { Badge, Spinner, Button, EmptyState } from "@/components/common";
import { useTranslation } from "react-i18next";

import { SystemSettingsSection } from "./SystemSettingsSection";

// Refresh cadence for the live stats. 30s matches the original behaviour
// and is frequent enough to feel live without flooding the dir-walk on
// the backend (image cache + transcode cache are filesystem walks).
const REFETCH_MS = 30_000;

// Friendly labels for the canonical content_type vocabulary. Anything we
// haven't named falls back to the raw value so a future "music" library
// renders as "music" instead of disappearing.
const CONTENT_TYPE_LABELS: Record<string, string> = {
  movies: "contentTypes.movies",
  shows: "contentTypes.tvShows",
  livetv: "contentTypes.liveTV",
};

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

// formatBytes — short human-readable size. Returns "—" for zero so
// missing/empty caches read clearly. Sub-MiB is integer; MiB+ is one
// decimal — the panel is at-a-glance, not a forensic tool.
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

// formatServerTime — local time only, no date (the date is implied by the
// 30s polling cadence). The TZ tag is rendered separately in the hint.
function formatServerTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return d.toLocaleTimeString();
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

// SystemStatus — "what's going on with my server right now" at a glance.
// Lives at /admin/system/status as the default sub-tab inside the System
// page. Mirrors what Plex puts under Status > Dashboard: the server's
// identity and a real-time health snapshot.
//
// Intentionally drops the Go-runtime power-user fields that the previous
// iteration showed (goroutines, GC pause, go_version, OS/arch). They are
// useful for debugging but noise for an admin who just wants to know
// the server is alive.
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

      <Section title={t("admin.system.sectionLibraries")}>
        <LibraryCards stats={stats} />
      </Section>

      <SystemSettingsSection />
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

  // Aggregate "is everything green?" — single traffic light. DB ping +
  // FFmpeg detection are the two an admin checks first when anything
  // misbehaves; weighting them equally surfaces the symptom immediately.
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

  // BaseURL hint: when empty the operator hasn't configured a public
  // URL — point at the editable section below instead of the (now
  // removed) "edit YAML" string. The actual editing surface lives in
  // SystemSettingsSection rendered at the bottom of the page.
  const baseURLValue = stats.server.base_url || "—";
  const baseURLHint = stats.server.base_url
    ? undefined
    : t("admin.system.baseURLUnset");

  return (
    <>
      <StatCard
        label={t("admin.system.version")}
        value={stats.server.version || "—"}
      />
      <StatCard
        label={t("admin.system.uptime")}
        value={<span className="tabular-nums">{formatUptime(stats.server.uptime_seconds)}</span>}
      />
      <StatCard
        label={t("admin.system.bindAddress")}
        value={<span className="font-mono text-sm">{stats.server.bind_address || "—"}</span>}
      />
      <StatCard
        label={t("admin.system.baseURL")}
        value={<span className="font-mono text-sm">{baseURLValue}</span>}
        hint={baseURLHint}
      />
      <StatCard
        label={t("admin.system.serverTime")}
        value={<span className="tabular-nums">{formatServerTime(stats.server.server_time)}</span>}
        hint={`${t("admin.system.timezone")}: ${stats.server.timezone}`}
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

  // HW accel: three distinct states surfaced here.
  //  1) Disabled in settings       → "Disabled" + pointer to settings section
  //  2) Enabled but none detected  → "None" + actionable host-side hint
  //  3) Enabled and selected       → uppercase ID + encoder hint
  const selected = stats.ffmpeg.hw_accel_selected;
  const enabled = stats.ffmpeg.hw_accel_enabled;
  let accelLabel: string;
  let accelHint: string | undefined;
  if (!enabled) {
    accelLabel = t("admin.system.hwAccelDisabledLabel");
    accelHint = t("admin.system.hwAccelDisabledPointer");
  } else if (!selected || selected === "none") {
    accelLabel = t("admin.system.hwAccelNone");
    accelHint = t("admin.system.hwAccelNoneHint");
  } else {
    accelLabel = selected.toUpperCase();
    accelHint = stats.ffmpeg.hw_accel_encoder
      ? `${t("admin.system.hwAccelEncoder")}: ${stats.ffmpeg.hw_accel_encoder}`
      : undefined;
  }

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
        hint={accelHint}
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
  // Intentionally slimmed down vs the previous iteration. The fields
  // dropped (goroutines, GC pause, go_version, OS/arch) are still
  // returned by the backend so a power user can curl them; they don't
  // belong on the default admin view.
  return (
    <>
      <StatCard
        label={t("admin.system.memoryAlloc")}
        value={<span className="tabular-nums">{r.memory_alloc_mb} MiB</span>}
        hint={`${t("admin.system.memorySys")}: ${r.memory_sys_mb} MiB`}
      />
      <StatCard
        label={t("admin.system.cpuCount")}
        value={<span className="tabular-nums">{r.cpu_count}</span>}
      />
    </>
  );
}

function StorageCards({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const s = stats.storage;
  // Cache hint: when the size is zero, instead of just showing "—" we
  // explain why (no scans yet, no transcodes yet). Avoids the "is this
  // broken?" confusion on a fresh install.
  const imageHint = s.image_dir_bytes > 0 ? s.image_dir_path : t("admin.system.cacheEmptyImages", { path: s.image_dir_path });
  const transcodeHint = s.transcode_cache_bytes > 0 ? s.transcode_cache_path : t("admin.system.cacheEmptyTranscodes", { path: s.transcode_cache_path });

  return (
    <>
      <StatCard
        label={t("admin.system.imageDir")}
        value={<span className="tabular-nums">{formatBytes(s.image_dir_bytes)}</span>}
        hint={imageHint}
      />
      <StatCard
        label={t("admin.system.transcodeCache")}
        value={<span className="tabular-nums">{formatBytes(s.transcode_cache_bytes)}</span>}
        hint={transcodeHint}
      />
      <StatCard
        label={t("admin.system.databaseSize")}
        value={<span className="tabular-nums">{formatBytes(stats.database.size_bytes)}</span>}
        hint={stats.database.path}
      />
    </>
  );
}

function LibraryCards({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const l = stats.libraries;

  // Total + per-type rollup. Backend already sorts by_type alphabetically
  // so the card order is stable across renders.
  return (
    <>
      <StatCard
        label={t("admin.system.totalLibraries")}
        value={<span className="tabular-nums">{l.total}</span>}
        hint={t("admin.system.totalItems", { count: l.items_total })}
      />
      {l.by_type.map((bucket) => {
        const labelKey = CONTENT_TYPE_LABELS[bucket.content_type];
        const heading = labelKey ? t(labelKey) : bucket.content_type;
        return (
          <StatCard
            key={bucket.content_type}
            label={heading}
            value={<span className="tabular-nums">{bucket.items}</span>}
            hint={t("admin.system.libraryBucketHint", { count: bucket.count })}
          />
        );
      })}
    </>
  );
}
