import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import {
  queryKeys,
  useChannelsWithoutEPG,
  useLibraryEPGSources,
  useScheduledJobs,
  useUnhealthyChannels,
} from "@/api/hooks";
import { useEventStream } from "@/hooks/useEventStream";
import type { LibraryEPGSource } from "@/api/types";
import { ChannelsWithoutEPGPanel } from "./ChannelsWithoutEPGPanel";
import { EPGSourcesPanel } from "./EPGSourcesPanel";
import { ScheduledJobsPanel } from "./ScheduledJobsPanel";
import { UnhealthyChannelsPanel } from "./UnhealthyChannelsPanel";

type HealthStatus = "ok" | "warning" | "critical" | "pending";
type TabKey = "sources" | "without-epg" | "unhealthy" | "schedule";

interface HealthReport {
  status: HealthStatus;
  label: string;
  detail: string;
}

/**
 * LivetvAdminPanel — single admin surface for every livetv library.
 * Replaces the old "three stacked panels" layout with:
 *
 *   1. Compact header: status dot + health label + stats strip.
 *   2. Tab switcher (sources / sin guía / con problemas). Tabs with a
 *      zero count are hidden entirely so the admin doesn't scan past
 *      irrelevant chrome.
 *   3. Body = whichever sub-panel matches the active tab.
 *
 * Auto-selects the problem tab on mount when the library is in a
 * warning state caused by unhealthy channels or orphan channels —
 * that's where the admin's attention is needed. Otherwise defaults to
 * "sources" because that's the primary configuration surface.
 */
export function LivetvAdminPanel({
  libraryId,
  totalChannels,
}: {
  libraryId: string;
  totalChannels: number;
}) {
  const { t } = useTranslation();
  const { data: sources = [] } = useLibraryEPGSources(libraryId);
  const { data: unhealthy = [] } = useUnhealthyChannels(libraryId);
  const { data: withoutEPG = [] } = useChannelsWithoutEPG(libraryId);
  const { data: schedule = [] } = useScheduledJobs(libraryId);

  // Real-time push: when the backend's prober/proxy/playback-failure
  // path flips a channel between health buckets it publishes
  // `channel.health.changed`. We invalidate the unhealthy-list and
  // channel-list caches so the admin sees the change without polling
  // and without a manual refresh. Filter on library_id so a flap on
  // another library doesn't churn this panel's queries.
  const queryClient = useQueryClient();
  // Coalesce a burst of events (e.g. a probe pass that flips many
  // channels back-to-back) into a single invalidation pair. Without
  // this, an N-channel burst yields N refetches even when the result
  // is identical — wasted bytes for the admin and the server.
  const invalidateTimerRef = useRef<number | null>(null);
  useEventStream("channel.health.changed", (raw) => {
    try {
      const evt = JSON.parse(raw) as { library_id?: string };
      if (evt.library_id && evt.library_id !== libraryId) return;
    } catch {
      // Malformed payload — fall through to invalidate. Cheap and
      // self-correcting; better than swallowing a real change.
    }
    if (invalidateTimerRef.current !== null) return;
    invalidateTimerRef.current = window.setTimeout(() => {
      invalidateTimerRef.current = null;
      queryClient.invalidateQueries({
        queryKey: queryKeys.unhealthyChannels(libraryId),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeys.channels(libraryId),
      });
    }, 250);
  });

  const health = useMemo<HealthReport>(
    () =>
      computeHealth(
        t,
        totalChannels,
        sources,
        unhealthy.length,
        withoutEPG.length,
      ),
    [t, totalChannels, sources, unhealthy.length, withoutEPG.length],
  );

  // Default tab is chosen by what the admin most likely wants to see:
  // if any channels are unhealthy, land on the "con problemas" tab;
  // otherwise start on "sources" (the primary config surface, also
  // where source-level errors show up via the warning dot on the tab).
  // The override state lets a click win — default is only the initial
  // guess, not a recurring nag.
  const [tabOverride, setTabOverride] = useState<TabKey | null>(null);
  const defaultTab: TabKey = unhealthy.length > 0 ? "unhealthy" : "sources";
  const tab = tabOverride ?? defaultTab;

  const epgCoveragePct =
    totalChannels > 0
      ? Math.round(((totalChannels - withoutEPG.length) / totalChannels) * 100)
      : 0;
  const matched = Math.max(0, totalChannels - withoutEPG.length);

  return (
    <div className="border border-border rounded-lg bg-bg-elevated/40">
      <header className="flex items-start gap-3 p-4 border-b border-border">
        <StatusDot status={health.status} />
        <div className="flex-1 min-w-0 space-y-2">
          <div className="flex items-baseline gap-2 flex-wrap">
            <span className="text-sm font-semibold text-text-primary">
              {health.label}
            </span>
            {health.detail ? (
              <span className="text-xs text-text-secondary">
                {health.detail}
              </span>
            ) : null}
          </div>
          <StatsStrip
            total={totalChannels}
            matched={matched}
            coveragePct={epgCoveragePct}
            unhealthy={unhealthy.length}
            withoutEPG={withoutEPG.length}
          />
        </div>
      </header>

      <TabBar
        libraryId={libraryId}
        active={tab}
        onSelect={setTabOverride}
        sourcesCount={sources.length}
        sourcesHaveErrors={sources.some((s) => s.last_status === "error")}
        withoutEPGCount={withoutEPG.length}
        unhealthyCount={unhealthy.length}
        scheduleEnabledCount={schedule.filter((j) => j.enabled).length}
        scheduleHasErrors={schedule.some((j) => j.last_status === "error")}
        // Always shown: the GET /schedule endpoint synthesises two
        // placeholder rows for libraries without schedules so the tab
        // has stable content from first paint. Gating on schedule.length
        // here would make the tab appear after the initial fetch, which
        // flickers the tab bar on load.
        showSchedule={true}
      />

      <div
        id={`livetv-panel-${libraryId}-${tab}`}
        role="tabpanel"
        aria-labelledby={`livetv-tab-${libraryId}-${tab}`}
        className="p-4"
      >
        {tab === "sources" ? (
          <EPGSourcesPanel libraryId={libraryId} />
        ) : null}
        {tab === "without-epg" && withoutEPG.length > 0 ? (
          <ChannelsWithoutEPGPanel libraryId={libraryId} />
        ) : null}
        {tab === "unhealthy" && unhealthy.length > 0 ? (
          <UnhealthyChannelsPanel libraryId={libraryId} />
        ) : null}
        {tab === "schedule" ? (
          <ScheduledJobsPanel libraryId={libraryId} />
        ) : null}
      </div>
    </div>
  );
}

// ─── Sub-components ──────────────────────────────────────────────────

interface TabBarProps {
  libraryId: string;
  active: TabKey;
  onSelect: (tab: TabKey) => void;
  sourcesCount: number;
  sourcesHaveErrors: boolean;
  withoutEPGCount: number;
  unhealthyCount: number;
  scheduleEnabledCount: number;
  scheduleHasErrors: boolean;
  showSchedule: boolean;
}

/**
 * TabBar — keyboard-accessible tab list following the WAI-ARIA
 * Authoring Practices pattern:
 *  - Only the active tab is in the tab stop (tabIndex=0); others are
 *    tabIndex=-1 so Tab jumps straight to the tabpanel.
 *  - ← / → cycle between visible tabs.
 *  - Home / End jump to first / last.
 *  - ids on each tab + aria-controls link to the tabpanel below so
 *    screen readers announce the relationship.
 */
function TabBar({
  libraryId,
  active,
  onSelect,
  sourcesCount,
  sourcesHaveErrors,
  withoutEPGCount,
  unhealthyCount,
  scheduleEnabledCount,
  scheduleHasErrors,
  showSchedule,
}: TabBarProps) {
  const { t } = useTranslation();
  const tabsRef = useRef<Array<HTMLButtonElement | null>>([]);

  const tabs: Array<{
    key: TabKey;
    label: string;
    count: number;
    tone: "default" | "warning";
    show: boolean;
  }> = [
    {
      key: "sources",
      label: t("admin.livetv.tabs.sources", { defaultValue: "Fuentes EPG" }),
      count: sourcesCount,
      tone: sourcesHaveErrors ? "warning" : "default",
      show: true,
    },
    {
      key: "schedule",
      label: t("admin.livetv.tabs.schedule", { defaultValue: "Programación" }),
      count: scheduleEnabledCount,
      tone: scheduleHasErrors ? "warning" : "default",
      show: showSchedule,
    },
    {
      key: "without-epg",
      label: t("admin.livetv.tabs.withoutEPG", { defaultValue: "Sin guía" }),
      count: withoutEPGCount,
      tone: "default",
      show: withoutEPGCount > 0,
    },
    {
      key: "unhealthy",
      label: t("admin.livetv.tabs.unhealthy", { defaultValue: "Con problemas" }),
      count: unhealthyCount,
      tone: "warning",
      show: unhealthyCount > 0,
    },
  ];
  const visible = tabs.filter((tb) => tb.show);

  function handleKeyDown(e: React.KeyboardEvent, currentIndex: number) {
    let nextIndex = currentIndex;
    if (e.key === "ArrowRight") nextIndex = (currentIndex + 1) % visible.length;
    else if (e.key === "ArrowLeft") nextIndex = (currentIndex - 1 + visible.length) % visible.length;
    else if (e.key === "Home") nextIndex = 0;
    else if (e.key === "End") nextIndex = visible.length - 1;
    else return;
    e.preventDefault();
    onSelect(visible[nextIndex].key);
    tabsRef.current[nextIndex]?.focus();
  }

  return (
    <div
      className="flex gap-1 px-3 pt-2 border-b border-border"
      role="tablist"
      aria-label={t("admin.livetv.tabsLabel", {
        defaultValue: "Secciones de la biblioteca livetv",
      })}
    >
      {visible.map((tb, i) => {
        const isActive = tb.key === active;
        const baseStyle = isActive
          ? "bg-bg-card text-text-primary border-b-2 border-accent -mb-px"
          : "text-text-secondary hover:text-text-primary hover:bg-bg-card/60";
        return (
          <button
            key={tb.key}
            ref={(el) => {
              tabsRef.current[i] = el;
            }}
            type="button"
            role="tab"
            id={`livetv-tab-${libraryId}-${tb.key}`}
            aria-selected={isActive}
            aria-controls={`livetv-panel-${libraryId}-${tb.key}`}
            tabIndex={isActive ? 0 : -1}
            onClick={() => onSelect(tb.key)}
            onKeyDown={(e) => handleKeyDown(e, i)}
            className={`px-3 py-2 text-sm rounded-t transition-colors ${baseStyle}`}
          >
            <span>{tb.label}</span>
            {tb.count > 0 ? (
              <span
                className={`ml-2 inline-flex items-center justify-center rounded-full px-1.5 py-0.5 text-[11px] tabular-nums ${
                  tb.tone === "warning"
                    ? "bg-warning/10 text-warning"
                    : "bg-accent/10 text-accent-light"
                }`}
              >
                {tb.count}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}

function StatusDot({ status }: { status: HealthStatus }) {
  const config = {
    ok: { color: "bg-success", ring: "ring-success/30" },
    warning: { color: "bg-warning", ring: "ring-warning/30" },
    critical: { color: "bg-error", ring: "ring-error/30" },
    pending: { color: "bg-text-muted", ring: "ring-text-muted/30" },
  }[status];

  return (
    <span
      className={`mt-1 flex h-2.5 w-2.5 shrink-0 rounded-full ring-4 ${config.color} ${config.ring}`}
      aria-hidden="true"
    />
  );
}

function StatsStrip({
  total,
  matched,
  coveragePct,
  unhealthy,
  withoutEPG,
}: {
  total: number;
  matched: number;
  coveragePct: number;
  unhealthy: number;
  withoutEPG: number;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-3 flex-wrap text-xs text-text-secondary">
      <span className="tabular-nums">
        <span className="font-medium text-text-primary">{total}</span>{" "}
        {t("admin.livetv.stats.channels", { defaultValue: "canales" })}
      </span>
      {total > 0 ? (
        <>
          <Separator />
          <span className="flex items-center gap-2 tabular-nums">
            <span>
              {t("admin.livetv.stats.epg", { defaultValue: "EPG" })}{" "}
              <span className="font-medium text-text-primary">{matched}</span>
              <span className="text-text-muted"> ({coveragePct}%)</span>
            </span>
            <span
              className="relative block h-1.5 w-20 rounded-full bg-border overflow-hidden"
              aria-hidden="true"
            >
              <span
                className="absolute inset-y-0 left-0 rounded-full bg-accent"
                style={{ width: `${coveragePct}%` }}
              />
            </span>
          </span>
        </>
      ) : null}
      {unhealthy > 0 ? (
        <>
          <Separator />
          <span className="tabular-nums text-warning">
            <span className="font-medium">{unhealthy}</span>{" "}
            {t("admin.livetv.stats.unhealthy", {
              defaultValue: "con problemas",
            })}
          </span>
        </>
      ) : null}
      {withoutEPG > 0 ? (
        <>
          <Separator />
          <span className="tabular-nums">
            <span className="font-medium text-text-primary">{withoutEPG}</span>{" "}
            {t("admin.livetv.stats.withoutEPG", { defaultValue: "sin guía" })}
          </span>
        </>
      ) : null}
    </div>
  );
}

function Separator() {
  return (
    <span className="text-text-muted" aria-hidden="true">
      ·
    </span>
  );
}

// ─── Health computation ──────────────────────────────────────────────
//
// Translates raw counts into a single status + human label so the
// header conveys "is this library in trouble?" at a glance.

// Coverage threshold below which the library is considered to have an
// "EPG gap" worth surfacing as a warning. 70% is generous: most catalog
// sources cover ~80%+ of a country playlist, so dropping under this
// usually means the source died, the wrong source is selected, or the
// playlist drifted (channel renamed upstream).
const epgCoverageWarnPct = 70;

function computeHealth(
  t: (key: string, opts?: Record<string, unknown>) => string,
  totalChannels: number,
  sources: LibraryEPGSource[],
  unhealthyCount: number,
  withoutEPGCount: number,
): HealthReport {
  if (totalChannels === 0) {
    return {
      status: "pending",
      label: t("admin.livetv.health.pending", {
        defaultValue: "Pendiente de importar",
      }),
      detail: t("admin.livetv.health.pendingHint", {
        defaultValue: 'Pulsa "Actualizar canales" para traer el M3U.',
      }),
    };
  }

  if (sources.length === 0) {
    return {
      status: "warning",
      label: t("admin.livetv.health.noSources", {
        defaultValue: "Sin fuente EPG",
      }),
      detail: t("admin.livetv.health.noSourcesHint", {
        defaultValue:
          "Los canales funcionan; la guía no aparecerá hasta añadir una fuente.",
      }),
    };
  }

  const failing = sources.filter((s) => s.last_status === "error").length;
  const neverRefreshed = sources.filter(
    (s) => s.last_status === "" || s.last_status === null,
  ).length;

  if (failing > 0 && failing === sources.length) {
    return {
      status: "critical",
      label: t("admin.livetv.health.allFailing", {
        defaultValue: "Todas las fuentes EPG fallan",
      }),
      detail: t("admin.livetv.health.allFailingHint", {
        defaultValue: "Revisa las URLs o añade una fuente alternativa.",
      }),
    };
  }

  const warnings: string[] = [];
  if (failing > 0) {
    warnings.push(
      t("admin.livetv.health.someFailing", {
        defaultValue: "{{count}} fuente(s) fallando",
        count: failing,
      }),
    );
  }
  if (unhealthyCount > 0) {
    warnings.push(
      t("admin.livetv.health.unhealthy", {
        defaultValue: "{{count}} canal(es) con fallos de conexión",
        count: unhealthyCount,
      }),
    );
  }
  // Low EPG coverage is a real problem: a "Live TV" tab that can't tell
  // you what's on now is half-broken. We only flag once the user has a
  // baseline (at least one source reported back), so a freshly-added
  // source doesn't immediately scream "warning".
  const epgPct =
    totalChannels > 0
      ? ((totalChannels - withoutEPGCount) / totalChannels) * 100
      : 100;
  const sourcesEverRefreshed = sources.some((s) => s.last_status === "ok");
  if (sourcesEverRefreshed && epgPct < epgCoverageWarnPct) {
    warnings.push(
      t("admin.livetv.health.lowEpgCoverage", {
        defaultValue: "cobertura EPG baja ({{pct}}%)",
        pct: Math.round(epgPct),
      }),
    );
  }
  if (warnings.length > 0) {
    return {
      status: "warning",
      label: t("admin.livetv.health.attention", {
        defaultValue: "Atención",
      }),
      detail: warnings.join(" · "),
    };
  }

  if (neverRefreshed === sources.length) {
    return {
      status: "pending",
      label: t("admin.livetv.health.neverRefreshed", {
        defaultValue: "EPG sin refrescar aún",
      }),
      detail: t("admin.livetv.health.neverRefreshedHint", {
        defaultValue: 'Pulsa "Actualizar EPG" para cargar la guía.',
      }),
    };
  }

  return {
    status: "ok",
    label: t("admin.livetv.health.ok", { defaultValue: "Todo en orden" }),
    detail: "",
  };
}
