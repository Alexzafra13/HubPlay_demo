import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheck, Info } from "lucide-react";

import type { User, UserPermissions } from "@/api/types";
import { useSetUserPermissions } from "@/api/hooks";
import { UserAvatar } from "@/components/common";

// AdminPermissionsMatrix pinta la matriz user × permission para los
// admins de la instalación. Filas: cada admin (role=admin, parent_user_id
// vacío). Columnas: los 7 flags granulares (can_upload se gestiona en
// la sección normal de usuario porque también aplica a no-admins).
//
// Reglas que el componente refleja visualmente:
//   - El administrador principal (is_owner=true) aparece con un badge
//     dorado y TODAS sus casillas marcadas + bloqueadas (gris). No hay
//     forma de cambiar nada — el backend rechazaría con OWNER_IMMUTABLE,
//     y aquí evitamos el round-trip a 403.
//   - can_manage_admins (la primera columna) sólo es editable si el
//     viewer es el owner. Para todos los demás, esa columna está
//     bloqueada con un tooltip que explica por qué.
//   - El resto de columnas son editables si el viewer es owner OR
//     tiene can_manage_admins.
//   - Si el viewer NO tiene can_manage_admins (ni es owner), TODA la
//     matriz es read-only — la sección sigue visible para que sepa
//     quién tiene qué, pero no puede tocar nada.
//
// El click sobre un checkbox dispara una mutación SETPERMS inmediata
// (no hay un "Guardar"). Es consistente con el resto de toggles del
// admin panel (active, content rating, library access).

interface PermissionColumn {
  key: keyof Pick<
    UserPermissions,
    | "can_manage_admins"
    | "can_manage_users"
    | "can_manage_libraries"
    | "can_manage_iptv"
    | "can_edit_metadata"
    | "can_change_artwork"
    | "can_view_audit"
  >;
  headerKey: string; // i18n key bajo admin.users.*
  descKey: string;
  ownerOnly: boolean; // sólo el owner puede otorgarlo a otros
}

const COLUMNS: PermissionColumn[] = [
  {
    key: "can_manage_admins",
    headerKey: "permissionsHeaderManageAdmins",
    descKey: "permissionsCanManageAdminsDesc",
    ownerOnly: true,
  },
  {
    key: "can_manage_users",
    headerKey: "permissionsHeaderManageUsers",
    descKey: "permissionsCanManageUsersDesc",
    ownerOnly: false,
  },
  {
    key: "can_manage_libraries",
    headerKey: "permissionsHeaderManageLibraries",
    descKey: "permissionsCanManageLibrariesDesc",
    ownerOnly: false,
  },
  {
    key: "can_manage_iptv",
    headerKey: "permissionsHeaderManageIPTV",
    descKey: "permissionsCanManageIPTVDesc",
    ownerOnly: false,
  },
  {
    key: "can_edit_metadata",
    headerKey: "permissionsHeaderEditMetadata",
    descKey: "permissionsCanEditMetadataDesc",
    ownerOnly: false,
  },
  {
    key: "can_change_artwork",
    headerKey: "permissionsHeaderChangeArtwork",
    descKey: "permissionsCanChangeArtworkDesc",
    ownerOnly: false,
  },
  {
    key: "can_view_audit",
    headerKey: "permissionsHeaderViewAudit",
    descKey: "permissionsCanViewAuditDesc",
    ownerOnly: false,
  },
];

interface AdminPermissionsMatrixProps {
  users: User[];
  // El usuario que ha invocado la pantalla — su is_owner +
  // can_manage_admins determinan qué celdas son editables.
  me: User | undefined;
}

export function AdminPermissionsMatrix({
  users,
  me,
}: AdminPermissionsMatrixProps) {
  const { t } = useTranslation();
  const setPerms = useSetUserPermissions();

  // Errores per-celda. Mapa (userId+col) → mensaje. Se borra al
  // siguiente click; no bloquea el resto de la matriz.
  const [errors, setErrors] = useState<Record<string, string>>({});

  // Filtramos a los admins cuenta-titular. Profiles (parent_user_id
  // != "") no son admins por construcción, pero un parent que SEA
  // admin sí entra.
  const admins = useMemo(
    () =>
      users.filter(
        (u) => u.role === "admin" && !u.parent_user_id,
      ),
    [users],
  );

  // El owner siempre primero — además del badge, el orden refuerza
  // visualmente "esta cuenta manda". Resto por created_at (orden
  // estable que ya viene del backend) — no re-ordenamos.
  const ordered = useMemo(() => {
    const owner = admins.find((u) => u.is_owner);
    const rest = admins.filter((u) => !u.is_owner);
    return owner ? [owner, ...rest] : admins;
  }, [admins]);

  // viewer puede editar la columna X si:
  //  - es owner (puede TODO), o
  //  - tiene can_manage_admins Y la columna NO es ownerOnly.
  function viewerCanEdit(col: PermissionColumn): boolean {
    if (!me) return false;
    if (me.is_owner) return true;
    if (!me.can_manage_admins) return false;
    return !col.ownerOnly;
  }

  function cellKey(userId: string, col: string) {
    return `${userId}::${col}`;
  }

  function onToggle(target: User, col: PermissionColumn, next: boolean) {
    if (target.is_owner) return; // double-defense: el checkbox ya está disabled
    setErrors((prev) => {
      const copy = { ...prev };
      delete copy[cellKey(target.id, col.key)];
      return copy;
    });
    setPerms.mutate(
      { userId: target.id, flags: { [col.key]: next } },
      {
        onError: (err) => {
          const msg = mapErrorToI18n(err.message, t);
          setErrors((prev) => ({
            ...prev,
            [cellKey(target.id, col.key)]: msg,
          }));
        },
      },
    );
  }

  if (ordered.length === 0) {
    return (
      <section className="rounded-lg border border-neutral-800 bg-neutral-900/40 p-6">
        <header className="flex items-center gap-2 mb-2">
          <ShieldCheck size={18} className="text-amber-400" />
          <h2 className="text-base font-semibold">
            {t("admin.users.permissionsMatrixTitle")}
          </h2>
        </header>
        <p className="text-sm text-neutral-400">
          {t("admin.users.permissionsMatrixEmpty")}
        </p>
      </section>
    );
  }

  return (
    <section className="rounded-lg border border-neutral-800 bg-neutral-900/40 p-4 sm:p-6">
      <header className="mb-3 flex items-start gap-2">
        <ShieldCheck size={18} className="text-amber-400 mt-0.5 shrink-0" />
        <div>
          <h2 className="text-base font-semibold">
            {t("admin.users.permissionsMatrixTitle")}
          </h2>
          <p className="text-sm text-neutral-400 mt-1">
            {t("admin.users.permissionsMatrixHint")}
          </p>
        </div>
      </header>

      {/* Tabla scrollable horizontalmente en mobile — 8 columnas no
          caben en 360px. En desktop fluye natural. */}
      <div className="overflow-x-auto -mx-4 sm:mx-0">
        <table className="w-full min-w-[760px] text-sm">
          <thead>
            <tr className="text-left border-b border-neutral-800">
              <th className="sticky left-0 bg-neutral-900/60 px-3 py-2 font-medium text-neutral-300 z-10">
                {t("admin.users.permissionsHeaderUser")}
              </th>
              {COLUMNS.map((col) => (
                <th
                  key={col.key}
                  className="px-3 py-2 font-medium text-neutral-300 align-bottom"
                  scope="col"
                >
                  <div className="flex items-center gap-1">
                    <span>{t(`admin.users.${col.headerKey}`)}</span>
                    <span
                      className="text-neutral-500 cursor-help"
                      title={t(`admin.users.${col.descKey}`)}
                      aria-label={t(`admin.users.${col.descKey}`)}
                    >
                      <Info size={12} aria-hidden />
                    </span>
                  </div>
                  {col.ownerOnly && (
                    <span className="block text-[10px] uppercase tracking-wide text-amber-500 mt-0.5">
                      {t("admin.users.permissionsOwnerBadge")}
                    </span>
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {ordered.map((u) => {
              const isOwner = u.is_owner === true;
              return (
                <tr
                  key={u.id}
                  className="border-b border-neutral-800/50 hover:bg-neutral-900/30"
                >
                  <td className="sticky left-0 bg-neutral-900/60 px-3 py-2 z-10">
                    <div className="flex items-center gap-2 min-w-[180px]">
                      <UserAvatar user={u} size="sm" />
                      <div className="flex flex-col">
                        <span className="font-medium truncate">
                          {u.display_name || u.username}
                        </span>
                        {isOwner && (
                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wide text-amber-400">
                            <ShieldCheck size={10} aria-hidden />
                            {t("admin.users.permissionsOwnerBadge")}
                          </span>
                        )}
                      </div>
                    </div>
                  </td>
                  {COLUMNS.map((col) => {
                    // Owner se muestra siempre marcado y bloqueado.
                    // Los demás dependen del valor + permisos del viewer.
                    const checked = isOwner ? true : !!u[col.key];
                    const editable = !isOwner && viewerCanEdit(col);
                    const err = errors[cellKey(u.id, col.key)];
                    return (
                      <td key={col.key} className="px-3 py-2">
                        <label
                          className={`inline-flex items-center justify-center w-7 h-7 rounded ${
                            editable
                              ? "cursor-pointer hover:bg-neutral-800"
                              : "cursor-not-allowed opacity-60"
                          }`}
                          title={
                            isOwner
                              ? t("admin.users.permissionsErrorOwnerImmutable")
                              : !editable && col.ownerOnly
                                ? t("admin.users.permissionsOwnerOnlyTooltip")
                                : undefined
                          }
                        >
                          <input
                            type="checkbox"
                            className="w-4 h-4 accent-amber-500"
                            checked={checked}
                            disabled={!editable}
                            onChange={(e) =>
                              onToggle(u, col, e.target.checked)
                            }
                            aria-label={`${u.username} — ${t(`admin.users.${col.headerKey}`)}`}
                          />
                        </label>
                        {err && (
                          <p className="text-[10px] text-red-400 mt-1 max-w-[120px]">
                            {err}
                          </p>
                        )}
                      </td>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

// mapErrorToI18n traduce los códigos de error que el backend
// (PermissionsHandler) emite a copy localizable. Cualquier código
// desconocido cae a un mensaje genérico — preferible a mostrar
// el text crudo del backend al usuario final.
function mapErrorToI18n(message: string, t: (k: string) => string): string {
  if (message.includes("OWNER_IMMUTABLE")) {
    return t("admin.users.permissionsErrorOwnerImmutable");
  }
  if (message.includes("OWNER_ONLY")) {
    return t("admin.users.permissionsErrorOwnerOnly");
  }
  return t("admin.users.permissionsErrorGeneric");
}
