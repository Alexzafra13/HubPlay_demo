import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Maximize2,
  Minimize2,
  Pause,
  Play,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { Button } from "@/components/common";

// LogsPanel — admin "Logs" surface, mounted under /admin/system →
// Avanzado. Backed by GET /admin/system/logs/stream (SSE) which:
//   1. replays the in-memory ring on subscribe so the panel isn't
//      empty for the first few seconds, then
//   2. pushes every new log entry as it lands.
//
// Rediseño con semantica visual (versus el dump monocromo previo):
//
//   - Borde-izquierda coloreado por level (info/warn/error). Mas
//     escaneable que prefijar "WARN" en monocromo.
//   - Chip de `module` extraido de attrs (auth, federation, iptv,
//     library, scanner...) - click filtra solo a ese module.
//   - Search box que filtra substring sobre msg + module + attrs
//     serializados. Cliente-side, cero round-trip.
//   - Multi-toggle de niveles (DEBUG/INFO/WARN/ERROR) en vez del
//     dropdown de 3 opciones. Cliclar cada uno on/off independiente.
//   - Click en entrada -> expande attrs como tabla key/value. Ahora
//     se aplastan a "k=v k2=v2" y la informacion util se pierde.
//   - Boton "copiar JSON" por entrada para compartir en un chat
//     o issue de GitHub sin reescribir.
//   - Boton fullscreen (overlay del viewport completo) para depurar
//     casos serios sin la limitacion de altura del panel.
//
// EventSource — not fetch+ReadableStream — porque cookie auth ya
// esta in place y EventSource maneja reconnect cuando un proxy mata
// idle SSE conns. Cero token pass-through (`withCredentials`).

interface LogEntry {
  ts: string;
  level: string;
  msg: string;
  attrs?: Record<string, unknown>;
}

const MAX_ENTRIES = 800;
const ALL_LEVELS = ["DEBUG", "INFO", "WARN", "ERROR"] as const;
type Level = (typeof ALL_LEVELS)[number];

export function LogsPanel() {
  const { t } = useTranslation();
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [connected, setConnected] = useState(false);
  const [search, setSearch] = useState("");
  // Niveles activos: por defecto INFO/WARN/ERROR. DEBUG fuera para
  // que un servidor con DEBUG abierto no inunde la vista; el admin
  // lo activa cuando quiere mirar bajo el capó.
  const [activeLevels, setActiveLevels] = useState<Set<Level>>(
    () => new Set<Level>(["INFO", "WARN", "ERROR"]),
  );
  // Filter por modulo: vacio = todos. Se rellena al click en un chip
  // de cualquier entrada.
  const [moduleFilter, setModuleFilter] = useState<string | null>(null);
  const [fullscreen, setFullscreen] = useState(false);

  // Buffered new entries while paused so the operator catches up
  // when they un-pause instead of missing whatever happened during.
  const bufferRef = useRef<LogEntry[]>([]);
  const [bufferedCount, setBufferedCount] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pausedRef = useRef(paused);

  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  // SSE wiring. Recreated only on mount/unmount; pause is handled
  // in the consumer rather than by tearing down the connection.
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
          if (bufferRef.current.length > MAX_ENTRIES) {
            bufferRef.current = bufferRef.current.slice(-MAX_ENTRIES);
          }
          setBufferedCount(bufferRef.current.length);
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

  const onTogglePause = () => {
    setPaused((prev) => {
      if (prev) {
        const drained = bufferRef.current;
        bufferRef.current = [];
        setBufferedCount(0);
        if (drained.length > 0) {
          setEntries((prevEntries) => {
            const next = prevEntries.concat(drained);
            return next.length > MAX_ENTRIES
              ? next.slice(-MAX_ENTRIES)
              : next;
          });
        }
      }
      return !prev;
    });
  };

  // Filtrado memoizado: levels + module + search en cascada.
  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    return entries.filter((e) => {
      if (!activeLevels.has(e.level as Level)) return false;
      const mod = moduleOf(e);
      if (moduleFilter && mod !== moduleFilter) return false;
      if (q.length > 0) {
        const hay = `${e.level} ${mod} ${e.msg} ${JSON.stringify(e.attrs ?? {})}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [entries, activeLevels, moduleFilter, search]);

  // Modulos vistos en el buffer actual — chips arriba para click-
  // filter sin tener que escribir el nombre.
  const knownModules = useMemo(() => {
    const set = new Set<string>();
    for (const e of entries) {
      const m = moduleOf(e);
      if (m) set.add(m);
    }
    return Array.from(set).sort();
  }, [entries]);

  // Auto-scroll a la nueva entrada solo cuando NO esta pausado y NO
  // estamos en plena exploracion (el user ha scrolleado hacia arriba).
  // Heuristica simple: si el scroll esta a menos de 32 px del fondo,
  // se asume "siguiendo en vivo"; si no, el user esta leyendo y no
  // queremos saltarle el cursor.
  useEffect(() => {
    if (paused) return;
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 32) {
      el.scrollTop = el.scrollHeight;
    }
  }, [visible, paused]);

  const toggleLevel = (lvl: Level) => {
    setActiveLevels((prev) => {
      const next = new Set(prev);
      if (next.has(lvl)) {
        next.delete(lvl);
      } else {
        next.add(lvl);
      }
      return next;
    });
  };

  const containerCls = fullscreen
    ? "fixed inset-4 z-50 flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5 shadow-2xl"
    : "flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-5";

  return (
    <div className={containerCls}>
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
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
          <ConnectionPill connected={connected} t={t} />
          <Button
            size="sm"
            variant="ghost"
            onClick={onTogglePause}
            title={
              paused
                ? t("admin.logs.resume", { defaultValue: "Reanudar" })
                : t("admin.logs.pause", { defaultValue: "Pausar" })
            }
          >
            {paused ? (
              <Play className="size-3.5" />
            ) : (
              <Pause className="size-3.5" />
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
            <Trash2 className="size-3.5" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setFullscreen((f) => !f)}
            title={
              fullscreen
                ? t("admin.logs.exitFullscreen", {
                    defaultValue: "Salir de pantalla completa",
                  })
                : t("admin.logs.fullscreen", {
                    defaultValue: "Pantalla completa",
                  })
            }
          >
            {fullscreen ? (
              <Minimize2 className="size-3.5" />
            ) : (
              <Maximize2 className="size-3.5" />
            )}
          </Button>
        </div>
      </div>

      {/* Toolbar: search + level toggles */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[200px]">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-text-muted" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("admin.logs.searchPlaceholder", {
              defaultValue: "Buscar en mensaje, módulo, atributos…",
            })}
            className="w-full rounded-md border border-border bg-bg-elevated pl-8 pr-8 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          {search && (
            <button
              type="button"
              onClick={() => setSearch("")}
              aria-label={t("admin.logs.clearSearch", {
                defaultValue: "Limpiar búsqueda",
              })}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-primary"
            >
              <X className="size-3" />
            </button>
          )}
        </div>
        <div className="flex items-center gap-1 rounded-md border border-border bg-bg-elevated p-0.5">
          {ALL_LEVELS.map((lvl) => (
            <LevelToggle
              key={lvl}
              level={lvl}
              active={activeLevels.has(lvl)}
              onClick={() => toggleLevel(lvl)}
            />
          ))}
        </div>
      </div>

      {/* Module chips — solo se muestran si hay modules en el buffer
          actual. Click activa el filtro; el chip activo lleva una X
          para limpiarlo. */}
      {knownModules.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[10px] uppercase tracking-wider text-text-muted">
            {t("admin.logs.modulesLabel", { defaultValue: "Módulos" })}
          </span>
          {knownModules.map((m) => {
            const active = moduleFilter === m;
            return (
              <button
                key={m}
                type="button"
                onClick={() => setModuleFilter(active ? null : m)}
                className={[
                  "inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-mono text-[10px] transition-colors",
                  active
                    ? "bg-accent/15 text-accent ring-1 ring-accent/30"
                    : "bg-bg-elevated text-text-secondary hover:bg-bg-hover hover:text-text-primary",
                ].join(" ")}
              >
                {m}
                {active && <X className="size-2.5" />}
              </button>
            );
          })}
        </div>
      )}

      {/* Log box */}
      <div
        ref={scrollRef}
        className={[
          "overflow-y-auto rounded-md border border-border-subtle bg-bg-base",
          fullscreen ? "flex-1" : "h-80",
        ].join(" ")}
      >
        {visible.length === 0 ? (
          <p className="px-4 py-8 text-center text-xs text-text-muted">
            {entries.length === 0
              ? t("admin.logs.empty", {
                  defaultValue:
                    "No hay entradas todavía. Se mostrarán aquí en cuanto el servidor escriba algo.",
                })
              : t("admin.logs.emptyFiltered", {
                  defaultValue:
                    "Ningún log coincide con los filtros actuales.",
                })}
          </p>
        ) : (
          <ul className="flex flex-col divide-y divide-border-subtle/40">
            {visible.map((e) => (
              // ts + level + msg da una key compuesta única por entry
              // sin depender del índice (que cambia al filtrar).
              <LogRow
                key={`${e.ts}-${e.level}-${e.msg}`}
                entry={e}
                onModuleClick={(m) => setModuleFilter(m)}
              />
            ))}
          </ul>
        )}
      </div>

      {/* Footer hints */}
      <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-text-muted">
        <span>
          {t("admin.logs.counter", {
            defaultValue: "{{v}} / {{n}} entradas",
            v: visible.length,
            n: entries.length,
          })}
        </span>
        {paused && bufferedCount > 0 && (
          <span>
            {t("admin.logs.pausedHint", {
              defaultValue:
                "Pausado · {{n}} entrada(s) en cola, se aplicarán al reanudar.",
              n: bufferedCount,
            })}
          </span>
        )}
      </div>
    </div>
  );
}

// ─── Connection pill ───────────────────────────────────────────────

function ConnectionPill({
  connected,
  t,
}: {
  connected: boolean;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  return (
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
          "size-1.5 rounded-full",
          connected ? "bg-success" : "bg-warning",
        ].join(" ")}
      />
      {connected
        ? t("admin.logs.live", { defaultValue: "En vivo" })
        : t("admin.logs.reconnecting", {
            defaultValue: "Reconectando",
          })}
    </span>
  );
}

// ─── Level toggle (compact pill-button) ─────────────────────────────

const LEVEL_STYLE: Record<Level, { active: string; inactive: string }> = {
  DEBUG: {
    active: "bg-bg-base text-text-muted",
    inactive: "text-text-muted/50",
  },
  INFO: {
    active: "bg-bg-base text-text-secondary",
    inactive: "text-text-muted/50",
  },
  WARN: {
    active: "bg-warning/15 text-warning",
    inactive: "text-text-muted/50",
  },
  ERROR: {
    active: "bg-error/15 text-error",
    inactive: "text-text-muted/50",
  },
};

function LevelToggle({
  level,
  active,
  onClick,
}: {
  level: Level;
  active: boolean;
  onClick: () => void;
}) {
  const cls = LEVEL_STYLE[level];
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={[
        "rounded px-2 py-0.5 text-[10px] font-bold uppercase transition-colors",
        active ? cls.active : cls.inactive,
      ].join(" ")}
    >
      {level}
    </button>
  );
}

// ─── LogRow ─────────────────────────────────────────────────────────

function LogRow({
  entry,
  onModuleClick,
}: {
  entry: LogEntry;
  onModuleClick: (mod: string) => void;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);

  const mod = moduleOf(entry);
  const attrsWithoutModule = useMemo(() => {
    if (!entry.attrs) return null;
    const copy: Record<string, unknown> = { ...entry.attrs };
    delete copy.module;
    return Object.keys(copy).length > 0 ? copy : null;
  }, [entry.attrs]);
  const hasDetail = !!attrsWithoutModule;

  const time = (() => {
    try {
      return new Date(entry.ts).toLocaleTimeString();
    } catch {
      return entry.ts;
    }
  })();

  const borderColour = (() => {
    switch (entry.level) {
      case "ERROR":
        return "var(--color-error)";
      case "WARN":
        return "var(--color-warning)";
      case "DEBUG":
        return "var(--color-border-subtle)";
      default:
        return "var(--color-accent)";
    }
  })();

  const levelTextCls = (() => {
    switch (entry.level) {
      case "ERROR":
        return "text-error";
      case "WARN":
        return "text-warning";
      case "DEBUG":
        return "text-text-muted/70";
      default:
        return "text-text-secondary";
    }
  })();

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      const payload = JSON.stringify(entry, null, 2);
      navigator.clipboard.writeText(payload);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      // navigator.clipboard puede fallar en contextos no-secure;
      // silenciamos en lugar de petar el row.
    }
  };

  return (
    <li
      className="group relative"
      style={{ borderLeft: `2px solid ${borderColour}` }}
    >
      <button
        type="button"
        onClick={() => hasDetail && setExpanded((e) => !e)}
        className={[
          "w-full px-3 py-2 text-left font-mono text-[11px] leading-relaxed",
          "hover:bg-bg-elevated/40 transition-colors",
          hasDetail ? "cursor-pointer" : "cursor-default",
        ].join(" ")}
      >
        <div className="flex items-baseline gap-2">
          <span className="flex-none text-text-muted tabular-nums">
            {time}
          </span>
          <span
            className={[
              "flex-none text-[9px] font-bold uppercase tracking-wider tabular-nums",
              levelTextCls,
            ].join(" ")}
            style={{ width: "44px" }}
          >
            {entry.level}
          </span>
          {mod && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onModuleClick(mod);
              }}
              className="flex-none rounded bg-bg-elevated px-1.5 py-0.5 text-[9px] font-mono text-text-secondary hover:bg-accent/10 hover:text-accent transition-colors"
            >
              {mod}
            </button>
          )}
          <span className="min-w-0 flex-1 break-words text-text-primary">
            {entry.msg}
          </span>
          {/* Acciones por entrada — solo visibles en hover para no
              competir con el contenido principal. */}
          <span className="flex-none flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
            <span
              role="button"
              tabIndex={0}
              onClick={handleCopy}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.stopPropagation();
                  handleCopy(e as unknown as React.MouseEvent);
                }
              }}
              title={t("admin.logs.copy", {
                defaultValue: "Copiar JSON",
              })}
              className="rounded p-1 text-text-muted hover:bg-bg-hover hover:text-text-primary cursor-pointer"
            >
              {copied ? (
                <Check className="size-3 text-success" />
              ) : (
                <Copy className="size-3" />
              )}
            </span>
            {hasDetail &&
              (expanded ? (
                <ChevronDown className="size-3 text-text-muted" />
              ) : (
                <ChevronRight className="size-3 text-text-muted" />
              ))}
          </span>
        </div>
        {/* Preview de attrs cuando NO esta expandido — formato
            compacto k=v para no romper la lectura rapida del msg. */}
        {!expanded && attrsWithoutModule && (
          <div className="mt-0.5 pl-[140px] text-text-muted/80 truncate">
            {Object.entries(attrsWithoutModule)
              .slice(0, 6)
              .map(([k, v]) => `${k}=${formatAttr(v)}`)
              .join(" · ")}
          </div>
        )}
      </button>
      {/* Detalle expandido: tabla key/value de TODOS los attrs en
          formato legible. Cubre el caso en el que el preview de arriba
          oculta info que el admin necesita ver (errores con stack
          traces, payloads largos). */}
      {expanded && attrsWithoutModule && (
        <div className="mx-3 mb-2 rounded-md border border-border-subtle bg-bg-elevated px-3 py-2">
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 font-mono text-[11px]">
            {Object.entries(attrsWithoutModule).map(([k, v]) => (
              <div
                key={k}
                className="contents"
              >
                <dt className="text-text-muted">{k}</dt>
                <dd className="text-text-primary break-all whitespace-pre-wrap">
                  {formatAttrPretty(v)}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      )}
    </li>
  );
}

// ─── Helpers ────────────────────────────────────────────────────────

// moduleOf extrae el campo "module" de attrs si esta presente. Los
// loggers del proyecto añaden module via .With("module", "auth")
// asi que es una convencion estable que podemos explotar.
function moduleOf(e: LogEntry): string {
  const v = e.attrs?.module;
  return typeof v === "string" ? v : "";
}

function formatAttr(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string") {
    return v.length > 60 ? `${v.slice(0, 57)}…` : v;
  }
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  try {
    const s = JSON.stringify(v);
    return s.length > 60 ? `${s.slice(0, 57)}…` : s;
  } catch {
    return String(v);
  }
}

// formatAttrPretty se usa SOLO en el panel expandido, sin truncar
// para que el admin pueda leer stack traces / payloads enteros.
function formatAttrPretty(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
