import { useHealth } from "@/api/hooks";
import { Badge, Spinner } from "@/components/common";

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
        <Badge variant="error">Unreachable</Badge>
        <p className="text-sm text-text-muted">
          {error?.message ?? "Unable to reach the server."}
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
          System Overview
        </h2>
        {dataUpdatedAt > 0 && (
          <span className="text-xs text-text-muted">
            Updated {new Date(dataUpdatedAt).toLocaleTimeString()}
          </span>
        )}
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        <StatCard
          label="Server Status"
          value={
            <Badge variant={isHealthy ? "success" : "error"}>
              {health.status}
            </Badge>
          }
        />

        <StatCard label="Version" value={health.version} />

        <StatCard label="Uptime" value={formatUptime(health.uptime)} />

        <StatCard
          label="Database"
          value={
            <Badge variant={dbOk ? "success" : "error"}>
              {health.database}
            </Badge>
          }
        />

        <StatCard
          label="FFmpeg"
          value={
            <Badge variant={ffmpegOk ? "success" : "error"}>
              {health.ffmpeg}
            </Badge>
          }
        />

        <StatCard
          label="Active Streams"
          value={
            <span className="tabular-nums">{health.active_streams}</span>
          }
        />

        <StatCard
          label="Active Transcodes"
          value={
            <span className="tabular-nums">{health.active_transcodes}</span>
          }
        />
      </div>
    </div>
  );
}
