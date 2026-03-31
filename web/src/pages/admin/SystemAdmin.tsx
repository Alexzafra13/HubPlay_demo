import { useHealth } from "@/api/hooks";
import { Badge, Spinner } from "@/components/common";
import { useTranslation } from 'react-i18next';

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0) parts.push(`${hours}h`);
  parts.push(`${minutes}m`);

  return parts.join(" ");
}

interface StatCardProps {
  label: string;
  value: React.ReactNode;
}

function StatCard({ label, value }: StatCardProps) {
  return (
    <div className="flex flex-col gap-2 rounded-[--radius-lg] bg-bg-card border border-border p-5">
      <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
        {label}
      </span>
      <span className="text-lg font-semibold text-text-primary">{value}</span>
    </div>
  );
}

export default function SystemAdmin() {
  const { t } = useTranslation();
  const {
    data: health,
    isLoading,
    error,
    dataUpdatedAt,
  } = useHealth({ refetchInterval: 30_000 });

  if (isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (error || !health) {
    return (
      <div className="flex flex-col items-center gap-3 py-16">
        <Badge variant="error">{t('admin.system.unreachable')}</Badge>
        <p className="text-sm text-text-muted">
          {error?.message ?? t('admin.system.unableToReach')}
        </p>
      </div>
    );
  }

  const isHealthy = health.status === "healthy";
  const dbOk = health.database === "ok" || health.database === "healthy";
  const ffmpegOk = health.ffmpeg === "ok" || health.ffmpeg === "found";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-text-primary">
          {t('admin.system.title')}
        </h2>
        {dataUpdatedAt > 0 && (
          <span className="text-xs text-text-muted">
            {t('admin.system.updated', { time: new Date(dataUpdatedAt).toLocaleTimeString() })}
          </span>
        )}
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        <StatCard
          label={t('admin.system.serverStatus')}
          value={
            <Badge variant={isHealthy ? "success" : "error"}>
              {health.status}
            </Badge>
          }
        />

        <StatCard label={t('admin.system.version')} value={health.version} />

        <StatCard label={t('admin.system.uptime')} value={formatUptime(health.uptime)} />

        <StatCard
          label={t('admin.system.database')}
          value={
            <Badge variant={dbOk ? "success" : "error"}>
              {health.database}
            </Badge>
          }
        />

        <StatCard
          label={t('admin.system.ffmpeg')}
          value={
            <Badge variant={ffmpegOk ? "success" : "error"}>
              {health.ffmpeg}
            </Badge>
          }
        />

        <StatCard
          label={t('admin.system.activeStreams')}
          value={
            <span className="tabular-nums">{health.active_streams}</span>
          }
        />

        <StatCard
          label={t('admin.system.activeTranscodes')}
          value={
            <span className="tabular-nums">{health.active_transcodes}</span>
          }
        />
      </div>
    </div>
  );
}
