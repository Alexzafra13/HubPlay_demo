// NowPlayingPanel — admin-only "what's streaming right now" panel
// rendered on the Dashboard tab.
//
// Mirrors the Plex/Jellyfin admin live-sessions surface: one row per
// active session, with user / item / profile / method / elapsed,
// plus a Kill button per row. Driven by SSE — useAdminStreamSessions
// invalidates on transcode.started/completed events so the table
// refreshes within ~50 ms of a session starting or stopping. The
// useNowTick(1000) below is just a local re-render trigger so the
// elapsed counters keep climbing between events; it makes no
// network requests.
//
// State variants:
//   isLoading + no cache → Spinner (first time the tab opens)
//   isError              → EmptyState with retry hint
//   data.length === 0    → EmptyState ("No sessions right now")
//   data.length >  0     → table with one row per session

import { useTranslation } from "react-i18next";
import { useAdminStreamSessions, useKillAdminStreamSession } from "@/api/hooks";
import type { AdminStreamSession } from "@/api/types";
import { Badge, EmptyState, Spinner } from "@/components/common";
import { useNowTick } from "@/hooks/useNowTick";

export function NowPlayingPanel() {
  const { t, i18n } = useTranslation();
  const { data, isLoading, isError } = useAdminStreamSessions();
  const killMutation = useKillAdminStreamSession();
  // 1 Hz local tick so the elapsed column climbs smoothly between
  // SSE-driven cache invalidations. Replaces the side-effect of the
  // 5s refetch that used to drive the re-render.
  useNowTick(1000);

  if (isLoading && !data) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    );
  }

  if (isError) {
    return (
      <EmptyState
        title={t("admin.dashboard.nowPlayingErrorTitle")}
        description={t("admin.dashboard.nowPlayingErrorHint")}
      />
    );
  }

  const sessions = data ?? [];
  if (sessions.length === 0) {
    return (
      <EmptyState
        title={t("admin.dashboard.nowPlayingEmptyTitle")}
        description={t("admin.dashboard.nowPlayingEmptyHint")}
      />
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-border-subtle">
      <table className="min-w-full text-sm">
        <thead className="bg-bg-elevated text-text-muted">
          <tr>
            <th className="px-3 py-2 text-left font-medium">{t("admin.dashboard.sessionsCol.user")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("admin.dashboard.sessionsCol.item")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("admin.dashboard.sessionsCol.method")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("admin.dashboard.sessionsCol.profile")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("admin.dashboard.sessionsCol.elapsed")}</th>
            <th className="px-3 py-2 text-right font-medium">{t("admin.dashboard.sessionsCol.actions")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-subtle">
          {sessions.map((s) => (
            <SessionRow
              key={s.session_id}
              session={s}
              locale={i18n.language}
              onKill={() => killMutation.mutate({ sessionID: s.session_id })}
              killing={killMutation.isPending && killMutation.variables?.sessionID === s.session_id}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface SessionRowProps {
  session: AdminStreamSession;
  locale: string;
  onKill: () => void;
  killing: boolean;
}

function SessionRow({ session, locale, onKill, killing }: SessionRowProps) {
  const { t } = useTranslation();
  return (
    <tr className="bg-bg-base hover:bg-bg-elevated/40">
      <td className="px-3 py-2">
        <div className="font-medium text-text-base">
          {session.username || session.user_id}
        </div>
        {session.username && (
          <div className="text-xs text-text-muted">{session.user_id}</div>
        )}
      </td>
      <td className="px-3 py-2">
        <div className="font-medium text-text-base">
          {session.item_title || session.item_id}
        </div>
        {session.item_type && (
          <div className="text-xs text-text-muted capitalize">
            {session.item_type}
          </div>
        )}
      </td>
      <td className="px-3 py-2">
        <MethodBadge method={session.method} />
      </td>
      <td className="px-3 py-2 text-text-muted">
        {session.profile || "—"}
      </td>
      <td className="px-3 py-2 text-text-muted">
        {formatElapsed(session.started_at, locale, t)}
      </td>
      <td className="px-3 py-2 text-right">
        <button
          type="button"
          onClick={onKill}
          disabled={killing}
          className="inline-flex items-center rounded-md bg-error/10 px-3 py-1 text-xs font-medium text-error hover:bg-error/20 disabled:opacity-60"
        >
          {killing ? t("admin.dashboard.killing") : t("admin.dashboard.kill")}
        </button>
      </td>
    </tr>
  );
}

// MethodBadge colour-codes the playback decision so an admin can spot
// at a glance how expensive each session is. Transcode is the costly
// path (ffmpeg + encode → warning), DirectStream is remux only (cheap
// → default neutral), and DirectPlay is the ideal "no server work"
// outcome (success).
function MethodBadge({ method }: { method: AdminStreamSession["method"] }) {
  const variant: "default" | "success" | "warning" =
    method === "Transcode" ? "warning" : method === "DirectPlay" ? "success" : "default";
  return <Badge variant={variant}>{method}</Badge>;
}

// formatElapsed renders "Xm Ys" or "Xh Ym" relative to now. Recomputed
// on every render — the useNowTick(1000) at the panel level re-renders
// once a second, so elapsed values climb smoothly between SSE-driven
// invalidations without any network traffic.
function formatElapsed(startedAtISO: string, locale: string, t: (k: string, opts?: Record<string, unknown>) => string): string {
  const startedAt = Date.parse(startedAtISO);
  if (Number.isNaN(startedAt)) return "—";
  const seconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
  if (seconds < 60) {
    return t("admin.dashboard.elapsedSeconds", { count: seconds });
  }
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  // Avoid unused-locale warning when ICU plural fallback isn't needed.
  void locale;
  return `${minutes}m`;
}
