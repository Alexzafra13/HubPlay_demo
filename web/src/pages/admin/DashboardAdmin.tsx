// DashboardAdmin — admin landing page ("Resumen") at /admin/dashboard.
//
// The previous iteration was a bento of stat cards: traffic-light
// header, Now Playing block, four-card inventory grid, two "quick
// action" buttons. Functional but visually monotone — it read as
// "AI-generated dashboard template" because every section was the
// same card-on-card-grid pattern.
//
// This redesign trades the bento for editorial blocks with distinct
// shapes: a single-line health strip up top, Now Playing as
// information-dense rows (avatar + title + progress + method), a
// two-column "this week" panel pairing a watch-activity sparkline
// with the top-5 most-watched leaderboard, and the catalogue summary
// rendered as one-line prose. The visual rhythm changes block by
// block so the eye doesn't fall asleep on a uniform card grid.

import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { Library, PlayCircle, TrendingUp } from "lucide-react";
import {
  useAdminStreamActivity,
  useAdminStreamSessions,
  useAdminTopItems,
  useSystemStats,
} from "@/api/hooks";
import type { SystemStats } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";
import { SectionHeader } from "@/components/admin/SectionHeader";
import { AreaTimeline } from "@/components/admin/dashboard/AreaTimeline";
import { NowPlayingPanel } from "./dashboard/NowPlayingPanel";

// formatUptime — Plex-style "12d 3h 7m". Reused in SystemStatus too;
// inlined here because lifting one helper to web/src/utils for two
// consumers is over-architecture (the file is internal admin-only).
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

// formatHoursMinutes — turns a watch-minutes integer into "8h 24m"
// for the at-a-glance number above the sparkline. Returns "0m" for
// zero so the Resumen always shows a definite figure rather than a
// "no data yet" placeholder when activity is genuinely zero.
function formatHoursMinutes(minutes: number, t: (key: string, opts?: Record<string, unknown>) => string): string {
  if (!minutes || minutes <= 0) return t("admin.summary.minutesShort", { minutes: 0 });
  const hours = Math.floor(minutes / 60);
  const rem = minutes % 60;
  if (hours === 0) return t("admin.summary.minutesShort", { minutes: rem });
  if (rem === 0) return t("admin.summary.hoursShort", { hours });
  return t("admin.summary.hoursMinutesShort", { hours, minutes: rem });
}

// formatBytesCompact — short "8.2 TB" / "320 GB" / "—". The Resumen
// inventory line wants a single short token; sub-MiB never appears
// in real catalogues so we don't bother formatting it.
function formatBytesCompact(n: number | undefined | null): string {
  if (!n || n <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i <= 1 ? `${Math.round(v)} ${units[i]}` : `${v.toFixed(1)} ${units[i]}`;
}

export default function DashboardAdmin() {
  const { t } = useTranslation();
  const {
    data: stats,
    isLoading,
    error,
  } = useSystemStats({ refetchInterval: 30_000 });
  const { data: activity } = useAdminStreamActivity(14);
  const { data: topItems } = useAdminTopItems(7, 5);
  // Inspeccionamos las sesiones aqui solo para decidir si el bloque
  // "Now Playing" merece existir. La query se dedupea con la que
  // hace NowPlayingPanel internamente (TanStack Query las junta por
  // queryKey), asi que no hay double-fetch. Si no hay nadie viendo
  // nada, devolvemos null para esa seccion entera y la pagina pasa
  // de health-strip directo a "Esta semana" sin un panel vacio en
  // medio.
  const { data: liveSessions } = useAdminStreamSessions();
  const hasLiveSessions = (liveSessions?.length ?? 0) > 0;

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
      {/* Health strip — one line of trust. No card chrome, no
          padding around its own bg — just a row of facts ending in
          a "ver detalles" link. The dot uses the success / error
          token so the eye registers state at peripheral vision. */}
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <span
          aria-hidden
          className="h-2 w-2 rounded-full"
          style={{
            background: allHealthy ? "var(--color-success)" : "var(--color-error)",
          }}
        />
        <span className="font-semibold text-text-primary">
          {allHealthy
            ? t("admin.summary.healthLineHealthy")
            : t("admin.summary.healthLineDegraded")}
        </span>
        <span className="text-text-muted">·</span>
        <span className="text-text-secondary">HubPlay {stats.server.version}</span>
        <span className="text-text-muted">·</span>
        <span className="text-text-secondary">
          {t("admin.summary.uptime", { uptime: formatUptime(stats.server.uptime_seconds) })}
        </span>
        <span className="ml-auto">
          <Link
            to="/admin/system"
            className="text-sm font-medium text-accent hover:underline"
          >
            {t("admin.summary.viewSystem")}
          </Link>
        </span>
      </header>

      {/* Now Playing — solo aparece cuando hay alguien reproduciendo.
          Si el server esta idle (caso comun en uso casero), nos
          ahorramos un panel vacio con header + descripcion + empty
          state, y la pagina pasa directa de "salud" a "esta semana".
          Cuando alguien arranca un stream, la seccion entera se
          monta (el panel internamente sigue siendo el de siempre,
          con SSE + kill mutation propios). */}
      {hasLiveSessions && (
        <section className="flex flex-col gap-4">
          <SectionHeader
            icon={PlayCircle}
            title={t("admin.summary.nowPlaying")}
            subtitle={t("admin.summary.nowPlayingSubtitle", {
              defaultValue: "Reproducciones en curso ahora mismo.",
            })}
          />
          <NowPlayingPanel />
        </section>
      )}

      {/* This-week panel — sparkline of watch activity + top-5
          most-watched leaderboard, side by side on lg. */}
      <section className="flex flex-col gap-4">
        <SectionHeader
          icon={TrendingUp}
          title={t("admin.summary.thisWeek")}
          subtitle={t("admin.summary.thisWeekSubtitle", {
            defaultValue:
              "Tiempo total visualizado y los títulos que más arrastran.",
          })}
        />
        <div className="grid gap-6 lg:grid-cols-2">
          <ActivityPanel activity={activity?.buckets ?? []} t={t} />
          <TopItemsPanel items={topItems?.items ?? []} t={t} />
        </div>
      </section>

      {/* Catalogue summary — one prose line + a small action chip.
          Plain-language copy beats four stat cards for skimming. */}
      <section className="flex flex-col gap-4">
        <SectionHeader
          icon={Library}
          title={t("admin.summary.catalogue")}
          subtitle={t("admin.summary.catalogueSubtitle", {
            defaultValue: "Tamaño del catálogo y de la base de datos.",
          })}
        />
        <CatalogueLine stats={stats} />
      </section>
    </div>
  );
}

interface ActivityPanelProps {
  activity: { date: string; watch_minutes: number; session_count: number }[];
  t: (key: string, opts?: Record<string, unknown>) => string;
}

function ActivityPanel({ activity, t }: ActivityPanelProps) {
  const totalMinutes = activity.reduce((s, b) => s + b.watch_minutes, 0);
  const totalSessions = activity.reduce((s, b) => s + b.session_count, 0);
  const headline = formatHoursMinutes(totalMinutes, t);
  // Recharts come una array plana - mapeamos las buckets del backend
  // a la shape {date, minutes} para que el tooltip muestre el dia
  // legible y el valor formateado en minutos.
  const chartData = activity.map((b) => ({
    date: b.date,
    minutes: b.watch_minutes,
  }));

  return (
    <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
          {t("admin.summary.watchTime")}
        </span>
        <span className="text-xs text-text-muted tabular-nums">
          {t("admin.summary.lastDays", { count: activity.length })}
        </span>
      </div>
      <div className="flex items-baseline justify-between gap-4">
        <span className="text-2xl font-semibold text-text-primary tabular-nums">
          {headline}
        </span>
        <span className="text-xs text-text-muted">
          {t("admin.summary.sessionsCaption", { count: totalSessions })}
        </span>
      </div>
      {/* AreaTimeline en lugar del Sparkline puro: misma silueta
          pero con tooltip on-hover (dia + minutos), gradient fill
          que comunica "magnitud" sin necesitar un eje Y explicito,
          y mismo color que el resto de charts del proyecto. */}
      <div className="h-24 w-full">
        <AreaTimeline
          data={chartData}
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
      </div>
    </div>
  );
}

interface TopItemsPanelProps {
  items: { id: string; type: "movie" | "series"; title: string; play_count: number }[];
  t: (key: string, opts?: Record<string, unknown>) => string;
}

function TopItemsPanel({ items, t }: TopItemsPanelProps) {
  // El leader tiene el max - lo usamos como denominador para que las
  // barras finas debajo de cada titulo comuniquen "este se ha visto
  // el doble que este otro" de un vistazo sin tener que leer numeros.
  const max = items.reduce((m, x) => Math.max(m, x.play_count), 1);
  return (
    <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
          {t("admin.summary.mostWatched")}
        </span>
        <span className="text-xs text-text-muted">
          {t("admin.summary.lastDays", { count: 7 })}
        </span>
      </div>
      {items.length === 0 ? (
        <p className="text-sm text-text-muted py-4">
          {t("admin.summary.noPlaysYet")}
        </p>
      ) : (
        <ol className="flex flex-col gap-2">
          {items.map((item, i) => {
            const href =
              item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;
            const pct = (item.play_count / max) * 100;
            return (
              <li key={item.id} className="flex flex-col gap-1">
                <div className="flex items-baseline gap-3">
                  <span className="w-5 text-right text-xs font-semibold tabular-nums text-text-muted">
                    {i + 1}
                  </span>
                  <Link
                    to={href}
                    className="flex-1 truncate text-sm text-text-primary hover:text-accent transition-colors"
                  >
                    {item.title}
                  </Link>
                  <span className="text-xs tabular-nums text-text-muted">
                    {t("admin.summary.playCount", { count: item.play_count })}
                  </span>
                </div>
                <div className="h-1 ml-8 overflow-hidden rounded-full bg-bg-elevated">
                  <div
                    className="h-full bg-accent/70 transition-all"
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}

// CatalogueLine — single sentence rendering the catalogue size in
// plain language. The previous iteration shipped four stat cards
// for the same information; one prose line reads at a glance and
// keeps the page scrollable.
function CatalogueLine({ stats }: { stats: SystemStats }) {
  const { t } = useTranslation();
  const l = stats.libraries;

  if (l.total === 0) {
    return (
      <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-dashed border-border bg-bg-card/50 p-5">
        <p className="text-sm text-text-secondary">
          {t("admin.dashboard.inventoryEmptyHint")}
        </p>
        <Link
          to="/admin/libraries"
          className="self-start rounded-[--radius-md] bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent-hover"
        >
          {t("admin.dashboard.actionGoToLibraries")}
        </Link>
      </div>
    );
  }

  // Build the per-type tail: "·  4 720 películas, 230 series, 80 canales".
  // Skips empty buckets so the sentence stays compact.
  const typeFragments = l.by_type
    .filter((b) => b.items > 0)
    .map((b) => {
      const labelKey =
        b.content_type === "movies"
          ? "admin.summary.moviesCount"
          : b.content_type === "shows"
            ? "admin.summary.seriesCount"
            : b.content_type === "livetv"
              ? "admin.summary.channelsCount"
              : "admin.summary.itemsCount";
      return t(labelKey, { count: b.items });
    });

  const dbSize = formatBytesCompact(stats.database.size_bytes);

  return (
    <p className="text-base leading-relaxed text-text-secondary">
      <span className="font-semibold text-text-primary tabular-nums">
        {l.items_total.toLocaleString()}
      </span>{" "}
      {t("admin.summary.itemsIn", { count: l.total })}{" "}
      {typeFragments.length > 0 && (
        <span className="text-text-muted">· {typeFragments.join(", ")}</span>
      )}{" "}
      <Link
        to="/admin/libraries"
        className="text-sm font-medium text-accent hover:underline"
      >
        {t("admin.summary.manageLibraries")} ›
      </Link>
      <br />
      <span className="text-sm text-text-muted">
        {t("admin.summary.databaseSize", { size: dbSize })}
      </span>
    </p>
  );
}
