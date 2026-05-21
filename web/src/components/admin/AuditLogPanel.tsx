import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ClipboardList,
  LogIn,
  LogOut,
  ShieldCheck,
  UserPlus,
  UserMinus,
  KeyRound,
  Library as LibraryIcon,
  Tv,
  Edit3,
  Image as ImageIcon,
  Globe,
  Save,
  Upload as UploadIcon,
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Filter,
} from "lucide-react";

import { useAuditLog, useAuditEventTypes } from "@/api/hooks";
import type { AuditLogEntry } from "@/api/types";
import { Button, EmptyState, Input, Spinner } from "@/components/common";

// AuditLogPanel — owner-OR-can_view_audit panel para consultar el
// audit log unificado (PR5).
//
// Layout:
//   [filtros sticky]
//      type dropdown | actor input | from / to date | search | apply
//   [tabla]
//      icono · tipo · actor · target · timestamp · IP
//      click en fila → drawer con payload completo + user agent
//   [footer]
//      "X de N" + paginación (prev / next)
//
// Decisiones de UX:
//  - Filtros se aplican al pulsar "Aplicar" (no on-change), porque
//    cada cambio dispara un query — escribir en el search box no
//    debe martillear la DB.
//  - El payload se renderiza tal cual (string JSON) por defecto;
//    cada event_type sabido (auth.login.ok, permission.changed,
//    etc.) tiene un pretty renderer que muestra los campos en lugar
//    del blob raw.
//  - Iconos por categoría — el eye scan rápido del operador es lo
//    que vale, no el texto.
//
// Página completa, no chunk dentro del system status. Va como una
// sección expandible al final de /admin/system (junto con
// LogsPanel/BackupPanel/etc.).

const PAGE_SIZE = 50;

export function AuditLogPanel() {
  const { t } = useTranslation();

  const [appliedFilters, setAppliedFilters] = useState<{
    type: string;
    actor: string;
    from: string;
    to: string;
    q: string;
    offset: number;
  }>({
    type: "",
    actor: "",
    from: "",
    to: "",
    q: "",
    offset: 0,
  });
  const [draftType, setDraftType] = useState("");
  const [draftActor, setDraftActor] = useState("");
  const [draftFrom, setDraftFrom] = useState("");
  const [draftTo, setDraftTo] = useState("");
  const [draftQ, setDraftQ] = useState("");

  const { data: types } = useAuditEventTypes();
  const { data, isLoading, error } = useAuditLog({
    type: appliedFilters.type || undefined,
    actor: appliedFilters.actor || undefined,
    from: appliedFilters.from
      ? new Date(appliedFilters.from).toISOString()
      : undefined,
    to: appliedFilters.to
      ? new Date(appliedFilters.to).toISOString()
      : undefined,
    q: appliedFilters.q || undefined,
    limit: PAGE_SIZE,
    offset: appliedFilters.offset,
  });

  const [openRow, setOpenRow] = useState<AuditLogEntry | null>(null);

  function applyFilters() {
    setAppliedFilters({
      type: draftType,
      actor: draftActor,
      from: draftFrom,
      to: draftTo,
      q: draftQ,
      offset: 0,
    });
  }

  function clearFilters() {
    setDraftType("");
    setDraftActor("");
    setDraftFrom("");
    setDraftTo("");
    setDraftQ("");
    setAppliedFilters({
      type: "",
      actor: "",
      from: "",
      to: "",
      q: "",
      offset: 0,
    });
  }

  function pageBack() {
    setAppliedFilters((prev) => ({
      ...prev,
      offset: Math.max(0, prev.offset - PAGE_SIZE),
    }));
  }

  function pageForward() {
    setAppliedFilters((prev) => ({
      ...prev,
      offset: prev.offset + PAGE_SIZE,
    }));
  }

  const total = data?.total ?? 0;
  const offset = appliedFilters.offset;
  const showingFrom = total > 0 ? offset + 1 : 0;
  const showingTo = Math.min(offset + PAGE_SIZE, total);
  const hasPrev = offset > 0;
  const hasNext = offset + PAGE_SIZE < total;

  return (
    <section
      className="rounded-[--radius-lg] border border-border bg-bg-elevated p-4 sm:p-6"
      aria-labelledby="audit-panel-title"
    >
      <header className="mb-4 flex items-start gap-2">
        <ClipboardList size={18} className="text-accent mt-0.5 shrink-0" aria-hidden />
        <div>
          <h2
            id="audit-panel-title"
            className="text-base font-semibold text-text-primary"
          >
            {t("admin.audit.title", { defaultValue: "Auditoría" })}
          </h2>
          <p className="text-sm text-text-muted mt-1">
            {t("admin.audit.hint", {
              defaultValue:
                "Historial de acciones sensibles: inicios de sesión, cambios de permisos, subidas, ediciones de catálogo. Se conserva 90 días.",
            })}
          </p>
        </div>
      </header>

      {/* ── Filtros ────────────────────────────────────────────── */}
      <div className="mb-4 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 rounded border border-border bg-bg-base p-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-text-muted text-xs">
            {t("admin.audit.filterType", { defaultValue: "Tipo de evento" })}
          </span>
          <select
            value={draftType}
            onChange={(e) => setDraftType(e.target.value)}
            className="rounded-md border border-border bg-bg-elevated px-2 py-1.5 text-sm"
          >
            <option value="">
              {t("admin.audit.filterTypeAll", { defaultValue: "Todos" })}
            </option>
            {(types ?? []).map((tp) => (
              <option key={tp} value={tp}>
                {tp}
              </option>
            ))}
            {/* Categorías prefix-matching como atajos */}
            <option value="auth.">auth.* (todos los de auth)</option>
            <option value="permission.">permission.* (todos los de permisos)</option>
            <option value="upload.">upload.* (todos los de uploads)</option>
            <option value="system.">system.* (todos los de sistema)</option>
          </select>
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-text-muted text-xs">
            {t("admin.audit.filterActor", { defaultValue: "Actor (user id)" })}
          </span>
          <Input
            value={draftActor}
            onChange={(e) => setDraftActor(e.target.value)}
            placeholder="u-alex"
            className="text-sm"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-text-muted text-xs">
            {t("admin.audit.filterSearch", { defaultValue: "Buscar en payload/IP/UA" })}
          </span>
          <Input
            value={draftQ}
            onChange={(e) => setDraftQ(e.target.value)}
            placeholder="192.168 / username / etc"
            className="text-sm"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-text-muted text-xs">
            {t("admin.audit.filterFrom", { defaultValue: "Desde" })}
          </span>
          <Input
            type="datetime-local"
            value={draftFrom}
            onChange={(e) => setDraftFrom(e.target.value)}
            className="text-sm"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-text-muted text-xs">
            {t("admin.audit.filterTo", { defaultValue: "Hasta" })}
          </span>
          <Input
            type="datetime-local"
            value={draftTo}
            onChange={(e) => setDraftTo(e.target.value)}
            className="text-sm"
          />
        </label>

        <div className="flex items-end gap-2">
          <Button onClick={applyFilters} size="sm">
            <Filter size={14} className="mr-1" aria-hidden />
            {t("admin.audit.apply", { defaultValue: "Aplicar" })}
          </Button>
          <Button onClick={clearFilters} variant="secondary" size="sm">
            {t("admin.audit.clear", { defaultValue: "Limpiar" })}
          </Button>
        </div>
      </div>

      {/* ── Resultados ─────────────────────────────────────────── */}
      {isLoading && <Spinner />}

      {error && (
        <EmptyState
          title={t("admin.audit.loadErrorTitle", {
            defaultValue: "No se pudo cargar el log",
          })}
          description={error.message}
        />
      )}

      {data && data.rows.length === 0 && (
        <EmptyState
          title={t("admin.audit.emptyTitle", {
            defaultValue: "Sin eventos",
          })}
          description={t("admin.audit.emptyDesc", {
            defaultValue:
              "No hay eventos que coincidan con los filtros. Prueba a ampliar la ventana temporal.",
          })}
        />
      )}

      {data && data.rows.length > 0 && (
        <>
          <div className="overflow-x-auto -mx-4 sm:mx-0">
            <table className="w-full min-w-[760px] text-sm">
              <thead>
                <tr className="text-left border-b border-border text-text-muted">
                  <th className="p-2 font-medium w-9"></th>
                  <th className="p-2 font-medium">
                    {t("admin.audit.colEvent", { defaultValue: "Evento" })}
                  </th>
                  <th className="p-2 font-medium">
                    {t("admin.audit.colActor", { defaultValue: "Actor" })}
                  </th>
                  <th className="p-2 font-medium">
                    {t("admin.audit.colTarget", { defaultValue: "Sobre" })}
                  </th>
                  <th className="p-2 font-medium">
                    {t("admin.audit.colWhen", { defaultValue: "Cuándo" })}
                  </th>
                  <th className="p-2 font-medium">IP</th>
                </tr>
              </thead>
              <tbody>
                {data.rows.map((row) => (
                  <tr
                    key={row.id}
                    onClick={() => setOpenRow(row)}
                    className="border-b border-border/50 hover:bg-bg-hover cursor-pointer"
                  >
                    <td className="p-2 text-text-muted">
                      <EventIcon type={row.event_type} />
                    </td>
                    <td className="p-2 font-mono text-xs">
                      {row.event_type}
                    </td>
                    <td className="p-2 text-xs">
                      <ActorCell row={row} />
                    </td>
                    <td className="p-2 text-xs">
                      <TargetCell row={row} />
                    </td>
                    <td className="p-2 text-xs text-text-muted">
                      {new Date(row.created_at).toLocaleString()}
                    </td>
                    <td className="p-2 text-xs font-mono text-text-muted">
                      {row.ip_address}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Footer paginación */}
          <div className="mt-3 flex items-center justify-between text-sm text-text-muted">
            <span>
              {t("admin.audit.showing", {
                from: showingFrom,
                to: showingTo,
                total,
                defaultValue: "Mostrando {{from}}–{{to}} de {{total}}",
              })}
            </span>
            <div className="flex gap-1">
              <button
                type="button"
                onClick={pageBack}
                disabled={!hasPrev}
                aria-label={t("common.previous", { defaultValue: "Anterior" })}
                className="rounded p-1.5 hover:bg-bg-hover disabled:opacity-30 disabled:cursor-not-allowed"
              >
                <ChevronLeft size={16} aria-hidden />
              </button>
              <button
                type="button"
                onClick={pageForward}
                disabled={!hasNext}
                aria-label={t("common.next", { defaultValue: "Siguiente" })}
                className="rounded p-1.5 hover:bg-bg-hover disabled:opacity-30 disabled:cursor-not-allowed"
              >
                <ChevronRight size={16} aria-hidden />
              </button>
            </div>
          </div>
        </>
      )}

      {/* Drawer del row */}
      {openRow && (
        <RowDetail row={openRow} onClose={() => setOpenRow(null)} />
      )}
    </section>
  );
}

// ─── EventIcon ──────────────────────────────────────────────────────

function EventIcon({ type }: { type: string }) {
  // Mapa prefix → componente. El eye scan rápido del operador
  // ("¿qué fue esto?") se beneficia mucho de iconos consistentes
  // por categoría.
  const cls = "text-text-muted";
  const sz = 14;
  if (type.startsWith("auth.login.ok"))
    return <LogIn size={sz} className="text-green-500" aria-hidden />;
  if (type.startsWith("auth.login.failed"))
    return <AlertTriangle size={sz} className="text-red-500" aria-hidden />;
  if (type.startsWith("auth.logout"))
    return <LogOut size={sz} className={cls} aria-hidden />;
  if (type.startsWith("permission."))
    return <ShieldCheck size={sz} className="text-amber-400" aria-hidden />;
  if (type === "user.created")
    return <UserPlus size={sz} className="text-green-500" aria-hidden />;
  if (type === "user.deleted")
    return <UserMinus size={sz} className="text-red-500" aria-hidden />;
  if (type.startsWith("user.password"))
    return <KeyRound size={sz} className="text-amber-400" aria-hidden />;
  if (type.startsWith("library."))
    return <LibraryIcon size={sz} className={cls} aria-hidden />;
  if (type.startsWith("iptv."))
    return <Tv size={sz} className={cls} aria-hidden />;
  if (type.startsWith("metadata."))
    return <Edit3 size={sz} className={cls} aria-hidden />;
  if (type.startsWith("artwork."))
    return <ImageIcon size={sz} className={cls} aria-hidden />;
  if (type.startsWith("cors."))
    return <Globe size={sz} className={cls} aria-hidden />;
  if (type.startsWith("upload."))
    return <UploadIcon size={sz} className={cls} aria-hidden />;
  if (type.startsWith("system."))
    return <Save size={sz} className="text-red-500" aria-hidden />;
  return <ClipboardList size={sz} className={cls} aria-hidden />;
}

// ─── Detail drawer ──────────────────────────────────────────────────

function RowDetail({
  row,
  onClose,
}: {
  row: AuditLogEntry;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  // Intenta parsear el payload como JSON para pintarlo formateado.
  // Si falla (raw string viejo, payload empty), lo muestra tal cual.
  const pretty = useMemo(() => {
    if (!row.payload) return "";
    try {
      return JSON.stringify(JSON.parse(row.payload), null, 2);
    } catch {
      return row.payload;
    }
  }, [row.payload]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
      className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4"
    >
      <div
        role="presentation"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        className="w-full max-w-2xl max-h-[80vh] overflow-auto rounded-lg border border-border bg-bg-base p-5 shadow-xl"
      >
        <div className="mb-3 flex items-start justify-between gap-3">
          <h3 className="text-base font-semibold flex items-center gap-2">
            <EventIcon type={row.event_type} />
            <span className="font-mono">{row.event_type}</span>
          </h3>
          <button
            type="button"
            onClick={onClose}
            className="text-text-muted hover:text-text-primary"
            aria-label={t("common.close", { defaultValue: "Cerrar" })}
          >
            ✕
          </button>
        </div>

        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-2 text-sm mb-4">
          <DLine label={t("admin.audit.detailWhen", { defaultValue: "Cuándo" })}>
            {new Date(row.created_at).toLocaleString()}
          </DLine>
          <DLine label={t("admin.audit.detailActor", { defaultValue: "Actor" })}>
            {row.actor_user_id ? (
              <div>
                {row.actor_username && (
                  <div className="font-medium">{row.actor_username}</div>
                )}
                <div className="font-mono text-xs text-text-muted">
                  {row.actor_user_id}
                </div>
              </div>
            ) : (
              <span className="text-text-muted italic">anónimo</span>
            )}
          </DLine>
          <DLine label={t("admin.audit.detailTarget", { defaultValue: "Sobre" })}>
            {row.target_type || row.target_id ? (
              <div>
                <div className="text-text-muted text-xs uppercase tracking-wide">
                  {row.target_type}
                </div>
                {row.target_type === "user" && row.target_username && (
                  <div className="font-medium">{row.target_username}</div>
                )}
                {row.target_id && (
                  <div className="font-mono text-xs text-text-muted break-all">
                    {row.target_id}
                  </div>
                )}
              </div>
            ) : (
              <span className="text-text-muted italic">–</span>
            )}
          </DLine>
          <DLine label="IP">{row.ip_address || "–"}</DLine>
          <DLine label={t("admin.audit.detailUA", { defaultValue: "Navegador" })} wide>
            <span className="text-xs">{row.user_agent || "—"}</span>
          </DLine>
        </dl>

        {pretty && (
          <div>
            <h4 className="text-xs uppercase tracking-wide text-text-muted mb-1">
              {t("admin.audit.detailPayload", { defaultValue: "Detalles" })}
            </h4>
            <pre className="rounded border border-border bg-bg-elevated p-3 text-xs font-mono overflow-x-auto">
              {pretty}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}

function DLine({
  label,
  children,
  wide,
}: {
  label: string;
  children: React.ReactNode;
  wide?: boolean;
}) {
  return (
    <div className={wide ? "sm:col-span-2" : undefined}>
      <dt className="text-xs uppercase tracking-wide text-text-muted">{label}</dt>
      <dd className="mt-0.5">{children}</dd>
    </div>
  );
}

function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max - 1) + "…";
}

// ActorCell / TargetCell — preferimos pintar el username (legible) con
// el UUID truncado debajo en gris. Si no hay username (user borrado o
// evento sin actor), caemos al UUID truncado solo. El UUID completo
// queda en title= para que el operador pueda copiarlo si hace falta.
function ActorCell({ row }: { row: AuditLogEntry }) {
  if (!row.actor_user_id) {
    return <span className="text-text-muted italic">anónimo</span>;
  }
  if (row.actor_username) {
    return (
      <div className="leading-tight">
        <div className="font-medium text-text-primary">{row.actor_username}</div>
        <div
          className="font-mono text-[10px] text-text-muted"
          title={row.actor_user_id}
        >
          {truncate(row.actor_user_id, 12)}
        </div>
      </div>
    );
  }
  return (
    <span className="font-mono" title={row.actor_user_id}>
      {truncate(row.actor_user_id, 16)}
    </span>
  );
}

function TargetCell({ row }: { row: AuditLogEntry }) {
  if (!row.target_type && !row.target_id) {
    return <span className="text-text-muted">–</span>;
  }
  const name =
    row.target_type === "user" && row.target_username
      ? row.target_username
      : null;
  return (
    <div className="leading-tight">
      <div className="flex items-center gap-1">
        <span className="text-text-muted text-[10px] uppercase tracking-wide">
          {row.target_type}
        </span>
        {name && (
          <span className="font-medium text-text-primary">{name}</span>
        )}
      </div>
      {row.target_id && (
        <div
          className="font-mono text-[10px] text-text-muted"
          title={row.target_id}
        >
          {truncate(row.target_id, 16)}
        </div>
      )}
    </div>
  );
}
