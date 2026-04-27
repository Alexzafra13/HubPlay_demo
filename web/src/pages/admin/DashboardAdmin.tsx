import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useSystemStats } from "@/api/hooks";
import { Badge, Spinner, EmptyState } from "@/components/common";

// formatUptime — Plex-style "12d 3h 7m". Shared with SystemStatus; kept
// duplicated here to avoid importing from a sibling page just for one
// helper. If a third caller appears, lift to web/src/utils.
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

// DashboardAdmin — landing page at /admin/dashboard. The first surface
// an admin sees, modeled after the Plex Dashboard: a single screen
// answering "is the server healthy?", "who's watching what?", "what's
// in my library?", and "is anything I should act on?".
//
// Phase A1 ships the scaffold with the live-data sections backed by
// the existing /admin/system/stats endpoint:
//   - Header with traffic-light health badge + version + uptime.
//
// Subsequent phases fill in the rest:
//   B → Now Playing rail (live sessions)
//   A2 → Library inventory rollup
//   F → Update available banner
//   E → Backup quick action
//   G → Force logout quick action
//
// Today the planned slots are visible as placeholders so the user can
// see the planned shape and we get end-to-end navigation working.
export default function DashboardAdmin() {
  const { t } = useTranslation();
  const {
    data: stats,
    isLoading,
    error,
  } = useSystemStats({ refetchInterval: 30_000 });

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
        title={t("admin.dashboard.unreachable")}
        description={error?.message ?? t("admin.system.unableToReach")}
      />
    );
  }

  const dbOk = stats.database.ok;
  const ffmpegOk = stats.ffmpeg.found;
  const allHealthy = dbOk && ffmpegOk;

  return (
    <div className="flex flex-col gap-8">
      {/* Header — single line of trust. The version + uptime answer
          "did the server restart recently?" without making the admin
          dig into Sistema → Estado. */}
      <header className="flex flex-wrap items-center gap-3 rounded-[--radius-lg] border border-border bg-bg-card px-5 py-4">
        <Badge variant={allHealthy ? "success" : "error"}>
          {allHealthy ? t("admin.system.healthy") : t("admin.system.degraded")}
        </Badge>
        <span className="text-base font-semibold text-text-primary">
          HubPlay {stats.server.version}
        </span>
        <span className="text-sm text-text-muted">
          {t("admin.dashboard.uptimeLabel")} {formatUptime(stats.server.uptime_seconds)}
        </span>
        <span className="ml-auto">
          <Link
            to="/admin/system/status"
            className="text-sm font-medium text-accent hover:underline"
          >
            {t("admin.dashboard.viewSystemDetails")}
          </Link>
        </span>
      </header>

      {/* Now Playing — filled in by Phase B (sessions endpoint). The
          placeholder is intentional: the planned shape is visible so
          the user can see where it'll land. */}
      <Section title={t("admin.dashboard.nowPlaying")}>
        <EmptyState
          title={t("admin.dashboard.nowPlayingComingSoon")}
          description={t("admin.dashboard.nowPlayingComingSoonHint")}
        />
      </Section>

      {/* Inventory — filled in by Phase A2 (libraries section in
          systemStats). */}
      <Section title={t("admin.dashboard.inventory")}>
        <EmptyState
          title={t("admin.dashboard.inventoryComingSoon")}
          description={t("admin.dashboard.inventoryComingSoonHint")}
        />
      </Section>

      {/* Quick actions — filled in by phases E/G. Today shows a single
          link to the libraries page (which already supports refresh). */}
      <Section title={t("admin.dashboard.quickActions")}>
        <div className="flex flex-wrap gap-3">
          <Link
            to="/admin/libraries"
            className="rounded-[--radius-md] border border-border bg-bg-card px-4 py-3 text-sm font-medium text-text-primary hover:bg-bg-elevated transition-colors"
          >
            {t("admin.dashboard.actionGoToLibraries")}
          </Link>
          <Link
            to="/admin/system/advanced"
            className="rounded-[--radius-md] border border-border bg-bg-card px-4 py-3 text-sm font-medium text-text-primary hover:bg-bg-elevated transition-colors"
          >
            {t("admin.dashboard.actionGoToAdvanced")}
          </Link>
        </div>
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
        {title}
      </h3>
      {children}
    </section>
  );
}
