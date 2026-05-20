// LibraryAccessCheckboxes — controlled list of library checkboxes used
// by the admin user-management surface. Reused by:
//   1. The "Add user" modal — pre-populates the new account's
//      library_access set in a single POST.
//   2. The "Edit library access" modal — replaces an existing
//      account's grant set.
//
// Kept dumb on purpose: no fetch, no mutation, no internal state. The
// parent owns `selectedIds` so the same component can run inside a
// fresh-form (Add) or a server-state-backed form (Edit) without
// branching internally. `disabled` flips the inputs read-only — used
// when the parent renders the inherited-from-parent view for a
// profile target.

import { useTranslation } from "react-i18next";
import type { Library } from "@/api/types";

interface Props {
  libraries: Library[];
  selectedIds: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  /** Optional override for the empty-state message — used by the
   *  fresh-install path where the admin hasn't added any library
   *  yet, so the user-creation modal renders a hint instead of a
   *  silent empty section. */
  emptyHint?: string;
}

export function LibraryAccessCheckboxes({
  libraries,
  selectedIds,
  onChange,
  disabled,
  emptyHint,
}: Props) {
  const { t } = useTranslation();
  const selected = new Set(selectedIds);

  function toggle(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    // Orden estable: emite los ids en el mismo orden en que aparecen
    // en `libraries`, así el payload no depende del orden de clicks
    // (importante para tests y para mantener el diff limpio).
    // flatMap = filter + map en una sola pasada.
    onChange(libraries.flatMap((l) => (next.has(l.id) ? [l.id] : [])));
  }

  if (libraries.length === 0) {
    return (
      <p className="text-xs text-text-muted italic">
        {emptyHint ??
          t("admin.users.libraryAccessNoLibraries", {
            defaultValue:
              "No hay bibliotecas creadas todavía. Crea una desde /admin/libraries y vuelve aquí para asignar acceso.",
          })}
      </p>
    );
  }

  return (
    <div
      role="group"
      aria-label={t("admin.users.libraryAccessAriaLabel", {
        defaultValue: "Bibliotecas accesibles para este usuario",
      })}
      className="flex flex-col gap-2 max-h-56 overflow-y-auto rounded-[--radius-md] border border-border bg-bg-elevated px-3 py-2"
    >
      {libraries.map((lib) => {
        const isChecked = selected.has(lib.id);
        return (
          <label
            key={lib.id}
            className={`flex items-center gap-2 text-sm select-none ${
              disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer"
            }`}
          >
            <input
              type="checkbox"
              checked={isChecked}
              disabled={disabled}
              onChange={() => toggle(lib.id)}
              className="accent-accent"
            />
            <span className="text-text-primary">{lib.name}</span>
            <span className="text-xs text-text-muted">
              ({t(`admin.libraries.type.${lib.content_type}`, {
                defaultValue: lib.content_type,
              })})
            </span>
          </label>
        );
      })}
    </div>
  );
}
