import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Pause, Play, Trash2 } from "lucide-react";
import { Button } from "@/components/common";

// LogsPanel — admin "Logs" surface, mounted under /admin/system →
// Avanzado. Backed by GET /admin/system/logs/stream (SSE) which:
//   1. replays the in-memory ring on subscribe so the panel isn't
//      empty for the first few seconds, then
//   2. pushes every new log entry as it lands.
//
// UI shape: a single scrollable log box with a small toolbar
// (level filter, pause toggle, clear). Auto-scrolls to the bottom
// while live; pause stops the auto-scroll AND visually freezes the
// "feels live" indicator so the operator can read a stack trace
// without it being yanked away by fresh entries.
//
// EventSource — not fetch+ReadableStream — because cookie auth is
// already in place and EventSource handles reconnect for us when
// the proxy in front kills idle SSE conns. We don't pass a token
// anywhere; the cookie rides along automatically (`withCredentials`).

interface LogEntry {
  ts: string;
  level: string;
  msg: string;
  attrs?: Record<string, unknown>;
}

const MAX_ENTRIES = 800;

// Subset filter — admins almost always want "errors only" or "all";
// granular debug filtering would be a lot of UI for little gain.
type LevelFilter = "ALL" | "WARN" | "ERROR";

export function LogsPanel() {
  const { t } = useTranslation();
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState<LevelFilter>("ALL");
  const [connected, setConnected] = useState(false);

  // Buffered new entries while paused so the operator catches up
  // when they un-pause instead of missing whatever happened during.
  const bufferRef = useRef<LogEntry[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  // SSE wiring. Recreated only on mount/unmount; pause is handled
  // in the consumer rather than by tearing down the connection
  // (cheaper + the server keeps a stable subscription).
  useEffect(() => {
    const es = new EventSource("/api/v1/admin/system/logs/stream", {
      withCredentials: true,
    });
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (ev) => {
      try {
        const e = JSON.parse(ev.data) as LogEntry;
        if (pausedRef.current) {
          bufferRef.current.push(e);
          // Cap the pause buffer too — a forgotten paused tab
          // shouldn't grow without bound.
          if (bufferRef.current.length > MAX_ENTRIES) {
            bufferRef.current = bufferRef.current.slice(-MAX_ENTRIES);
          }
        } else {
          setEntries((prev) => {
            const next = prev.concat(e);
            return next.length > MAX_ENTRIES
              ? next.slice(-MAX_ENTRIES)
              : next;
          });
        }
      } catch {
        // Malformed payload — skip rather than tear down the stream.
      }
    };
    return () => es.close();
  }, []);

  // Drain the pause buffer when the operator un-pauses.
  useEffect(() => {
    if (!paused && bufferRef.current.length > 0) {
      const drained = bufferRef.current;
      bufferRef.current = [];
      setEntries((prev) => {
        const next = prev.concat(drained);
        return next.length > MAX_ENTRIES ? next.slice(-MAX_ENTRIES) : next;
      });
    }
  }, [paused]);

  // Auto-scroll. Only when not paused so the operator's read
  // position stays put while they investigate.
  useEffect(() => {
    if (paused) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [entries, paused]);

  const visible = useMemo(() => {
    if (filter === "ALL") return entries;
    if (filter === "WARN") {
      return entries.filter((e) => e.level === "WARN" || e.level === "ERROR");
    }
    return entries.filter((e) => e.level === "ERROR");
  }, [entries, filter]);

  return (
    <div className="flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-text-primary">
            {t("admin.logs.title", { defaultValue: "Logs en vivo" })}
          </h3>
          <p className="mt-1 text-xs text-text-muted">
            {t("admin.logs.subtitle", {
              defaultValue:
                "Últimos eventos del servidor. Útil para diagnosticar fallos sin abrir la consola del contenedor.",
            })}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={[
              "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-medium",
              connected
                ? "bg-success/15 text-success"
                : "bg-warning/15 text-warning",
            ].join(" ")}
            title={
              connected
                ? t("admin.logs.connected", { defaultValue: "Conectado" })
                : t("admin.logs.disconnected", {
                    defaultValue: "Reconectando…",
                  })
            }
          >
            <span
              className={[
                "h-1.5 w-1.5 rounded-full",
                connected ? "bg-success" : "bg-warning",
              ].join(" ")}
            />
            {connected
              ? t("admin.logs.live", { defaultValue: "En vivo" })
              : t("admin.logs.reconnecting", {
                  defaultValue: "Reconectando",
                })}
          </span>

          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value as LevelFilter)}
            className="rounded-md border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none"
          >
            <option value="ALL">
              {t("admin.logs.filterAll", { defaultValue: "Todo" })}
            </option>
            <option value="WARN">
              {t("admin.logs.filterWarn", {
                defaultValue: "Warn + Error",
              })}
            </option>
            <option value="ERROR">
              {t("admin.logs.filterError", { defaultValue: "Solo Error" })}
            </option>
          </select>

          <Button
            size="sm"
            variant="ghost"
            onClick={() => setPaused((p) => !p)}
            title={
              paused
                ? t("admin.logs.resume", { defaultValue: "Reanudar" })
                : t("admin.logs.pause", { defaultValue: "Pausar" })
            }
          >
            {paused ? (
              <Play className="h-3.5 w-3.5" />
            ) : (
              <Pause className="h-3.5 w-3.5" />
            )}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setEntries([])}
            title={t("admin.logs.clear", {
              defaultValue: "Limpiar pantalla",
            })}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <div
        ref={scrollRef}
        className="h-72 overflow-y-auto rounded-md border border-border-subtle bg-bg-base p-3 font-mono text-[11px] leading-relaxed"
      >
        {visible.length === 0 ? (
          <p className="text-text-muted">
            {t("admin.logs.empty", {
              defaultValue: "No hay entradas todavía. Se mostrarán aquí en cuanto el servidor escriba algo.",
            })}
          </p>
        ) : (
          visible.map((e, i) => <LogRow key={i} entry={e} />)
        )}
      </div>

      {paused && bufferRef.current.length > 0 && (
        <p className="text-[11px] text-text-muted">
          {t("admin.logs.pausedHint", {
            defaultValue:
              "Pausado · {{n}} entrada(s) en cola, se aplicarán al reanudar.",
            n: bufferRef.current.length,
          })}
        </p>
      )}
    </div>
  );
}

function LogRow({ entry }: { entry: LogEntry }) {
  const tone =
    entry.level === "ERROR"
      ? "text-error"
      : entry.level === "WARN"
        ? "text-warning"
        : entry.level === "DEBUG"
          ? "text-text-muted"
          : "text-text-secondary";
  const time = (() => {
    try {
      return new Date(entry.ts).toLocaleTimeString();
    } catch {
      return entry.ts;
    }
  })();
  return (
    <div className={`break-words ${tone}`}>
      <span className="text-text-muted">{time}</span>{" "}
      <span className="font-semibold uppercase">{entry.level}</span>{" "}
      <span className="text-text-primary">{entry.msg}</span>
      {entry.attrs && Object.keys(entry.attrs).length > 0 && (
        <span className="ml-1 text-text-muted">
          {Object.entries(entry.attrs)
            .map(([k, v]) => `${k}=${formatAttr(v)}`)
            .join(" ")}
        </span>
      )}
    </div>
  );
}

function formatAttr(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}
