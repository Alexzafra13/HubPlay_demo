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
import {
  Film,
  HardDrive,
  Library,
  PlayCircle,
  Radio,
  Sparkles,
  Tv,
  TrendingUp,
} from "lucide-react";
import {
  useAdminRecentlyAdded,
  useAdminStorageDisks,
  useAdminStreamActivity,
  useAdminStreamSessions,
  useAdminTopItems,
  useSystemStats,
} from "@/api/hooks";
import type {
  AdminDisk,
  AdminRecentlyAddedItem,
  SystemStats,
} from "@/api/types";
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
  // Storage breakdown: peso por biblioteca + uso del disco fisico.
  // Cadencia 60s (los discos no cambian en tiempo real, solo con
  // scans). Si el endpoint falla por cualquier motivo, los bloques
  // que lo usan caen a "—" en vez de romper la pagina.
  const { data: storage } = useAdminStorageDisks();
  // "Recientemente añadido" - ultimos 12 items en cualquier biblioteca.
  // El hook ya existe y lo usa Home; React Query dedupe por queryKey
  // asi que no hay double-fetch si el admin tambien tiene Home en
  // cache.
  // "Recientemente añadido" - endpoint dedicado admin que mezcla
  // movies + series rolled-up por actividad. NO usar useLatestItems
  // (devuelve episodios sueltos saturando el strip). El backend
  // agrupa al nivel correcto: cada card es una pelicula o una serie,
  // y las series con actividad en 14d llevan new_episodes_count >0
  // que pintamos como badge "+N nuevos".
  const { data: recentlyAdded } = useAdminRecentlyAdded(12);
  const latestItems = recentlyAdded?.items ?? [];

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

      {/* Recientemente añadido — strip horizontal de posters de lo
          ultimo escaneado en cualquier biblioteca. Solo aparece si
          hay items; en un server vacio se omite la seccion entera
          (mismo principio que Now Playing - cero ruido cuando no
          hay nada que decir). */}
      {latestItems.length > 0 && (
        <section className="flex flex-col gap-4">
          <SectionHeader
            icon={Sparkles}
            title={t("admin.summary.recentlyAdded", {
              defaultValue: "Recientemente añadido",
            })}
            subtitle={t("admin.summary.recentlyAddedSubtitle", {
              defaultValue:
                "Lo último que ha procesado el scanner en cualquier biblioteca.",
            })}
          />
          <RecentlyAddedStrip items={latestItems} />
        </section>
      )}

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

      {/* Catalogue — mini cards por content_type + linea inferior
          con el uso del disco fisico (gopsutil/disk.Usage). Es el
          "ultimo bloque editorial" - cuanto contenido tienes,
          repartido por tipo, y cuanto disco te queda. */}
      <section className="flex flex-col gap-4">
        <SectionHeader
          icon={Library}
          title={t("admin.summary.catalogue")}
          subtitle={t("admin.summary.catalogueSubtitle", {
            defaultValue: "Tamaño del catálogo y de la base de datos.",
          })}
        />
        <CatalogueBlock stats={stats} disks={storage?.disks ?? []} />
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

// RecentlyAddedStrip — carrusel horizontal de posters de los items
// mas recientes (cualquier biblioteca). Diseño "Plex feel" sin
// imitar Plex: posters compactos con titulo debajo, scroll horizontal
// nativo en mobile, grid en desktop. Click va al detalle.
//
// Por que un strip y no un grid: el bloque tiene que dar "el server
// acaba de procesar estas cosas" sin canibalizar la pagina. Un grid
// 4x3 ocuparia demasiado; un strip de altura fija comunica lo mismo
// con la mitad de pixels. Si el operador quiere ver mas, hay un link
// "ver biblioteca completa" implicito al hacer click en cualquier
// item.
function RecentlyAddedStrip({ items }: { items: AdminRecentlyAddedItem[] }) {
  const { t } = useTranslation();
  return (
    <div className="-mx-1 overflow-x-auto pb-1">
      <ul className="flex gap-3 px-1">
        {items.map((it) => {
          const href = recentlyAddedHref(it);
          const newCount = it.new_episodes_count ?? 0;
          return (
            <li key={it.id} className="flex-none w-28 sm:w-32">
              <Link to={href} className="group flex flex-col gap-1.5">
                <div className="relative aspect-[2/3] overflow-hidden rounded-md border border-border-subtle bg-bg-elevated">
                  {it.poster_url ? (
                    <img
                      src={it.poster_url}
                      alt=""
                      loading="lazy"
                      className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                    />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-text-muted">
                      <Film className="h-6 w-6" />
                    </div>
                  )}
                  {/* Badge "+N nuevos" en la esquina sup-der del
                      poster cuando una serie ha recibido capitulos
                      en los ultimos 14 dias. Plex-feel literal -
                      el operador ve al instante donde hay novedad
                      sin tener que abrir la serie. */}
                  {newCount > 0 && (
                    <span
                      className="absolute top-1.5 right-1.5 rounded-full bg-accent px-1.5 py-0.5 text-[9px] font-bold leading-none text-bg-base shadow-md"
                      title={t("admin.summary.newEpisodesTooltip", {
                        defaultValue: "{{n}} episodios nuevos en 14 días",
                        n: newCount,
                      })}
                    >
                      +{newCount}
                    </span>
                  )}
                </div>
                <p
                  className="truncate text-xs font-medium text-text-primary group-hover:text-accent transition-colors"
                  title={it.title}
                >
                  {it.title}
                </p>
                {/* Subtitle: si la serie tiene actividad, contamos
                    los capitulos nuevos en texto. Si es una pelicula
                    o serie sin actividad reciente, mostramos
                    año / tipo como antes. */}
                {newCount > 0 ? (
                  <p className="text-[10px] text-accent">
                    {t("admin.summary.newEpisodes", {
                      defaultValue: "{{n}} episodios nuevos",
                      n: newCount,
                    })}
                  </p>
                ) : it.year ? (
                  <p className="text-[10px] text-text-muted tabular-nums">
                    {it.year}
                  </p>
                ) : (
                  <p className="text-[10px] text-text-muted">
                    {prettyType(it.type, t)}
                  </p>
                )}
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function recentlyAddedHref(it: AdminRecentlyAddedItem): string {
  // El endpoint /admin/system/recently-added solo devuelve types
  // "movie" o "series" (los episodios estan rolled-up a su serie
  // padre). Routeo simple:
  if (it.type === "series") return `/series/${it.id}`;
  return `/movies/${it.id}`;
}

function prettyType(
  type: string,
  t: (k: string, opts?: Record<string, unknown>) => string,
): string {
  switch (type) {
    case "movie":
      return t("admin.summary.typeMovie", { defaultValue: "Película" });
    case "series":
      return t("admin.summary.typeSeries", { defaultValue: "Serie" });
    case "season":
      return t("admin.summary.typeSeason", { defaultValue: "Temporada" });
    case "episode":
      return t("admin.summary.typeEpisode", { defaultValue: "Episodio" });
    default:
      return type;
  }
}

// CatalogueBlock — el bloque inferior del dashboard. Reemplaza la
// prosa plana anterior por:
//
//   1. 3 mini cards (Películas / Series / Canales) con icon + count
//      + bar relativa al mayor. Click va a /admin/libraries.
//   2. Una linea fina con DB size + total bibliotecas + (si hay datos
//      de storage) "X TB usados / Y TB en N discos".
//
// Si el catalogo esta vacio (total === 0) cae a un empty state con
// CTA "Crear biblioteca", mismo patron que la version anterior.
function CatalogueBlock({
  stats,
  disks,
}: {
  stats: SystemStats;
  disks: AdminDisk[];
}) {
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

  // Solo las buckets con items > 0 para no pintar "0 series" cuando
  // el operador todavia no ha creado esa biblioteca.
  const buckets = l.by_type.filter((b) => b.items > 0);
  const maxItems = buckets.reduce((m, b) => Math.max(m, b.items), 1);

  // Agregado de discos: sumamos used + total de TODOS los mounts
  // unicos. Si una biblioteca vive en /mnt/media y otra en /mnt/livetv,
  // mostramos suma. Mas honesto que reportar solo un disco.
  const diskTotal = disks.reduce((s, d) => s + d.total_bytes, 0);
  const diskUsed = disks.reduce((s, d) => s + d.used_bytes, 0);
  const diskPct = diskTotal > 0 ? (diskUsed / diskTotal) * 100 : 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-3">
        {buckets.map((b) => (
          <ContentTypeCard
            key={b.content_type}
            contentType={b.content_type}
            items={b.items}
            max={maxItems}
            t={t}
          />
        ))}
      </div>

      {/* Footer: DB + total bibliotecas + (opcional) disk usage.
          Una sola linea, muted, separada por punto medio. */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-text-muted">
        <span>
          {t("admin.summary.librariesCount", {
            defaultValue: "{{count}} bibliotecas",
            count: l.total,
          })}
        </span>
        <span>·</span>
        <span>
          {t("admin.summary.databaseSize", {
            size: formatBytesCompact(stats.database.size_bytes),
          })}
        </span>
        {disks.length > 0 && diskTotal > 0 && (
          <>
            <span>·</span>
            <span className="inline-flex items-center gap-1.5">
              <HardDrive className="h-3 w-3" />
              {t("admin.summary.diskUsage", {
                defaultValue: "{{used}} de {{total}} ({{pct}}%)",
                used: formatBytesCompact(diskUsed),
                total: formatBytesCompact(diskTotal),
                pct: diskPct.toFixed(0),
              })}
            </span>
          </>
        )}
        <span className="ml-auto">
          <Link
            to="/admin/libraries"
            className="text-accent hover:underline"
          >
            {t("admin.summary.manageLibraries")} ›
          </Link>
        </span>
      </div>
    </div>
  );
}

// ContentTypeCard — uno de los 3 mini cards del catalogo. Icono
// tinted por tipo + count grande + bar relativa al maximo del row.
function ContentTypeCard({
  contentType,
  items,
  max,
  t,
}: {
  contentType: string;
  items: number;
  max: number;
  t: (k: string, opts?: Record<string, unknown>) => string;
}) {
  const meta = contentTypeMeta(contentType, t);
  const Icon = meta.icon;
  const pct = max > 0 ? (items / max) * 100 : 0;
  return (
    <Link
      to="/admin/libraries"
      className="group flex flex-col gap-2.5 rounded-[--radius-lg] border border-border bg-bg-card p-4 transition-colors hover:border-border-strong"
    >
      <div className="flex items-center gap-2">
        <div className="rounded-md bg-bg-elevated p-1.5 text-text-secondary group-hover:text-accent transition-colors">
          <Icon className="h-3.5 w-3.5" />
        </div>
        <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
          {meta.label}
        </span>
      </div>
      <span className="text-2xl font-semibold text-text-primary tabular-nums">
        {items.toLocaleString()}
      </span>
      <div className="h-1 w-full overflow-hidden rounded-full bg-bg-elevated">
        <div
          className="h-full bg-accent/70 transition-all"
          style={{ width: `${pct}%` }}
        />
      </div>
    </Link>
  );
}

function contentTypeMeta(
  contentType: string,
  t: (k: string, opts?: Record<string, unknown>) => string,
): { icon: typeof Film; label: string } {
  switch (contentType) {
    case "movies":
      return {
        icon: Film,
        label: t("admin.summary.typeMoviesLabel", {
          defaultValue: "Películas",
        }),
      };
    case "shows":
      return {
        icon: Tv,
        label: t("admin.summary.typeShowsLabel", {
          defaultValue: "Series",
        }),
      };
    case "livetv":
      return {
        icon: Radio,
        label: t("admin.summary.typeLiveTVLabel", {
          defaultValue: "Live TV",
        }),
      };
    default:
      return { icon: Library, label: contentType };
  }
}
