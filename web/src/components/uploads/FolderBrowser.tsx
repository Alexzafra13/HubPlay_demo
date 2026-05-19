import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Folder,
  FolderOpen,
  FolderPlus,
  FileVideo,
  ChevronRight,
  ChevronUp,
  Check,
  Pencil,
  Trash2,
  X,
  Home,
  Library as LibraryIcon,
  Loader2,
} from "lucide-react";

import {
  useUploadBrowse,
  useCreateUploadFolder,
  useDeleteUploadEntry,
  useRenameUploadEntry,
} from "@/api/hooks";
import type { Library } from "@/api/types";
import { Button, Input } from "@/components/common";

// FolderBrowser — explorador de carpetas estilo SFTP/Termius dentro
// de una librería destino (PR6 file explorer).
//
// Layout:
//   ┌──────────────────────────────────────────────────────────┐
//   │ Librería [▼ Movies         ]                              │
//   ├──────────────────────────────────────────────────────────┤
//   │ Home / Movies / Drama                       [↑ Subir]    │
//   ├──────────────────────────────────────────────────────────┤
//   │ 📁 Action                                                 │
//   │ 📁 Comedy                                                 │
//   │ 📁 Drama                                                  │
//   │ [+ Nueva carpeta]                                          │
//   └──────────────────────────────────────────────────────────┘
//
// State machine:
//   - libraryID: la librería seleccionada (de la lista que el caller
//     pasa).
//   - path: ruta canónica DENTRO de la librería (vacío = raíz).
//
// La selección actual se notifica al caller via onChange tras cada
// navegación. El padre decide si mostrar "Subir aquí" o aplicar la
// selección.

interface FolderBrowserProps {
  libraries: Library[];
  libraryID: string;
  path: string;
  onChange: (libraryID: string, path: string) => void;
  /**
   * Callback opcional: cuando el usuario arrastra ficheros DIRECTAMENTE
   * sobre una carpeta del browser (o el home/breadcrumb), el componente
   * llama aquí con el subpath de destino. El padre decide qué hacer —
   * típicamente encolar + autoarrancar el upload sin pasar por el
   * dropzone general. Sin este callback, drop sobre carpetas se ignora
   * silenciosamente.
   */
  onDropFiles?: (files: File[], subpath: string) => void;
}

export function FolderBrowser({
  libraries,
  libraryID,
  path,
  onChange,
  onDropFiles,
}: FolderBrowserProps) {
  const { t } = useTranslation();
  const { data, isLoading, error } = useUploadBrowse(libraryID, path, !!libraryID);
  const createFolder = useCreateUploadFolder();

  const [showNewFolder, setShowNewFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [folderError, setFolderError] = useState<string | null>(null);
  // dropTarget: subpath que está siendo hovered con un drag activo.
  // Necesario porque cuando un fichero pasa SOBRE una carpeta, los
  // eventos dragOver burbujean y queremos pintar exactamente UNA
  // carpeta destacada (la que está debajo del cursor), no todas.
  const [dropTarget, setDropTarget] = useState<string | null>(null);

  // Inline rename — guarda el subpath de la entry siendo renombrada
  // y el nombre nuevo que el usuario escribe.  Una sola entry a la
  // vez (cancela cualquier rename previo si el usuario empieza otro).
  const [renameTarget, setRenameTarget] = useState<{
    fullPath: string;
    isDir: boolean;
    newName: string;
  } | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const deleteEntry = useDeleteUploadEntry();
  const renameEntry = useRenameUploadEntry();

  function beginRename(fullPath: string, currentName: string, isDir: boolean) {
    setActionError(null);
    setRenameTarget({ fullPath, isDir, newName: currentName });
  }

  function cancelRename() {
    setRenameTarget(null);
    setActionError(null);
  }

  async function confirmRename() {
    if (!renameTarget) return;
    const newName = renameTarget.newName.trim();
    if (!newName) return;
    // Compone el path destino reemplazando sólo el último segmento
    // del fullPath. Permite mover-en-sitio sin tener que recordar
    // los segmentos parent.
    const parts = renameTarget.fullPath.split("/").filter(Boolean);
    parts.pop();
    parts.push(newName);
    const newFullPath = parts.join("/");
    if (newFullPath === renameTarget.fullPath) {
      cancelRename();
      return;
    }
    setActionError(null);
    try {
      await renameEntry.mutateAsync({
        libraryID,
        from: renameTarget.fullPath,
        to: newFullPath,
        parentPath: path,
      });
      cancelRename();
    } catch (err) {
      setActionError(
        err instanceof Error && err.message
          ? err.message
          : t("uploads.folder.renameError", {
              defaultValue: "No se pudo renombrar.",
            }),
      );
    }
  }

  async function handleDelete(fullPath: string, name: string, isDir: boolean) {
    setActionError(null);
    const msg = isDir
      ? t("uploads.folder.confirmDeleteDir", {
          name,
          defaultValue: `¿Borrar la carpeta "${name}" y todo su contenido?`,
        })
      : t("uploads.folder.confirmDeleteFile", {
          name,
          defaultValue: `¿Borrar "${name}"?`,
        });
    if (!confirm(msg)) return;
    try {
      await deleteEntry.mutateAsync({
        libraryID,
        path: fullPath,
        // Carpetas siempre recursive desde la UI — el usuario YA
        // confirmó el modal ("y todo su contenido"). Para ficheros
        // el flag es irrelevante (no-op).
        recursive: isDir,
        parentPath: path,
      });
    } catch (err) {
      setActionError(
        err instanceof Error && err.message
          ? err.message
          : t("uploads.folder.deleteError", {
              defaultValue: "No se pudo borrar.",
            }),
      );
    }
  }

  // dragSupported: el padre wireó onDropFiles. Si no, no pintamos los
  // affordances de drop (sería confuso ver carpetas que parecen drop
  // targets pero no responden).
  const dragSupported = !!onDropFiles;

  function handleFolderDragOver(e: React.DragEvent<HTMLElement>, target: string) {
    if (!dragSupported) return;
    // Sin preventDefault, el navegador rechaza el drop por default.
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = "copy";
    setDropTarget(target);
  }

  function handleFolderDragLeave(e: React.DragEvent<HTMLElement>) {
    if (!dragSupported) return;
    e.stopPropagation();
    // Sólo limpiamos si salimos del propio elemento — los hijos
    // generan leave events espurios que harían parpadear el estado.
    if (e.currentTarget === e.target) {
      setDropTarget(null);
    }
  }

  function handleFolderDrop(e: React.DragEvent<HTMLElement>, target: string) {
    if (!dragSupported || !onDropFiles) return;
    e.preventDefault();
    e.stopPropagation();
    setDropTarget(null);
    const files = Array.from(e.dataTransfer.files ?? []);
    if (files.length > 0) {
      onDropFiles(files, target);
    }
  }

  // Breadcrumb segments. Si path es "Movies/Drama", devuelve
  // [{name: "Movies", path: "Movies"}, {name: "Drama", path: "Movies/Drama"}].
  // El "Home" siempre es el primero (path: "") fuera del array para
  // que tenga un icono distinto.
  const breadcrumbs = useMemo(() => {
    if (!path) return [];
    const parts = path.split("/").filter(Boolean);
    return parts.map((name, i) => ({
      name,
      path: parts.slice(0, i + 1).join("/"),
    }));
  }, [path]);

  function enterFolder(p: string) {
    onChange(libraryID, p);
  }

  function goUp() {
    if (!path) return;
    const parts = path.split("/").filter(Boolean);
    parts.pop();
    onChange(libraryID, parts.join("/"));
  }

  async function handleCreateFolder(e: React.FormEvent) {
    e.preventDefault();
    setFolderError(null);
    const trimmed = newFolderName.trim();
    if (!trimmed) return;
    const newPath = path ? `${path}/${trimmed}` : trimmed;
    try {
      await createFolder.mutateAsync({
        libraryID,
        path: newPath,
        parentPath: path,
      });
      setNewFolderName("");
      setShowNewFolder(false);
      // Navega a la nueva carpeta tras crearla — el caso happy es
      // "crear + subir ahí dentro".
      onChange(libraryID, newPath);
    } catch (err) {
      setFolderError(
        err instanceof Error && err.message
          ? err.message
          : t("uploads.folder.createError", {
              defaultValue: "No se pudo crear la carpeta.",
            }),
      );
    }
  }

  return (
    <section className="rounded-[--radius-md] border border-border bg-bg-elevated overflow-hidden">
      {/* Library selector */}
      <header className="flex items-center gap-2 border-b border-border bg-bg-base px-3 py-2">
        <LibraryIcon size={14} className="text-text-muted shrink-0" aria-hidden />
        <span className="text-xs text-text-muted">
          {t("uploads.folder.libraryLabel", { defaultValue: "Biblioteca" })}
        </span>
        <select
          value={libraryID}
          onChange={(e) => onChange(e.target.value, "")}
          className="flex-1 rounded-md border border-border bg-bg-elevated px-2 py-1 text-sm"
        >
          {libraries.map((lib) => (
            <option key={lib.id} value={lib.id}>
              {lib.name}
            </option>
          ))}
        </select>
      </header>

      {/* Breadcrumb + back. Cada segmento es ADEMÁS drop target del
          drag&drop: el operador puede arrastrar a "Movies" estando
          dentro de "Movies/Drama" para subir un nivel arriba sin
          tener que navegar. */}
      <div className="flex items-center gap-1 border-b border-border bg-bg-base px-3 py-2 text-sm overflow-x-auto">
        <button
          type="button"
          onClick={() => enterFolder("")}
          onDragOver={(e) => handleFolderDragOver(e, "")}
          onDragLeave={handleFolderDragLeave}
          onDrop={(e) => handleFolderDrop(e, "")}
          aria-label={t("uploads.folder.root", { defaultValue: "Raíz" })}
          className={[
            "flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 transition-all",
            dropTarget === "" && dragSupported
              ? "bg-accent/20 ring-2 ring-accent scale-110"
              : "hover:bg-bg-hover",
          ].join(" ")}
        >
          <Home size={13} aria-hidden />
        </button>
        {breadcrumbs.map((bc) => (
          <span key={bc.path} className="flex shrink-0 items-center gap-1">
            <ChevronRight size={12} className="text-text-muted" aria-hidden />
            <button
              type="button"
              onClick={() => enterFolder(bc.path)}
              onDragOver={(e) => handleFolderDragOver(e, bc.path)}
              onDragLeave={handleFolderDragLeave}
              onDrop={(e) => handleFolderDrop(e, bc.path)}
              className={[
                "rounded px-1.5 py-0.5 truncate max-w-[160px] transition-all",
                dropTarget === bc.path && dragSupported
                  ? "bg-accent/20 ring-2 ring-accent text-accent font-medium"
                  : "hover:bg-bg-hover",
              ].join(" ")}
            >
              {bc.name}
            </button>
          </span>
        ))}
        {path && (
          <button
            type="button"
            onClick={goUp}
            aria-label={t("uploads.folder.up", { defaultValue: "Subir un nivel" })}
            className="ml-auto shrink-0 rounded p-1 text-text-muted hover:bg-bg-hover hover:text-text-primary"
          >
            <ChevronUp size={14} aria-hidden />
          </button>
        )}
      </div>

      {/* Lista de subdirs */}
      <div className="max-h-48 overflow-y-auto">
        {isLoading && (
          <div className="flex items-center justify-center p-4 text-text-muted">
            <Loader2 size={14} className="animate-spin" aria-hidden />
          </div>
        )}

        {error && (
          <p className="px-3 py-2 text-sm text-red-400" role="alert">
            {error.message}
          </p>
        )}

        {data && data.directories.length === 0 && !isLoading && !dragSupported && (
          <p className="px-3 py-3 text-xs text-text-muted italic">
            {t("uploads.folder.empty", {
              defaultValue: "Esta carpeta no tiene subcarpetas. Sube aquí o crea una nueva.",
            })}
          </p>
        )}

        {data && data.directories.length > 0 && (
          <ul className="py-1">
            {data.directories.map((d) => {
              const isDropOver = dropTarget === d.path && dragSupported;
              const isRenaming = renameTarget?.fullPath === d.path;
              return (
                <li
                  key={d.path}
                  onDragOver={(e) => handleFolderDragOver(e, d.path)}
                  onDragLeave={handleFolderDragLeave}
                  onDrop={(e) => handleFolderDrop(e, d.path)}
                  className={[
                    "group/row relative transition-all duration-150",
                    isDropOver
                      ? "bg-accent/15 ring-1 ring-inset ring-accent scale-[1.01]"
                      : "",
                  ].join(" ")}
                >
                  {isRenaming ? (
                    <RenameRow
                      icon={
                        <Folder
                          size={14}
                          className="text-text-muted shrink-0"
                          aria-hidden
                        />
                      }
                      value={renameTarget!.newName}
                      onChange={(v) =>
                        setRenameTarget({ ...renameTarget!, newName: v })
                      }
                      onConfirm={confirmRename}
                      onCancel={cancelRename}
                      isPending={renameEntry.isPending}
                    />
                  ) : (
                    <div className="flex items-center">
                      <button
                        type="button"
                        onClick={() => enterFolder(d.path)}
                        className={[
                          "flex flex-1 items-center gap-2 px-3 py-2 text-left text-sm transition-colors",
                          isDropOver
                            ? "text-accent font-medium"
                            : "hover:bg-bg-hover",
                        ].join(" ")}
                      >
                        {isDropOver ? (
                          <FolderOpen
                            size={16}
                            className="text-accent shrink-0 drop-shadow-[0_0_4px_rgba(var(--accent-rgb,234,179,8),0.4)]"
                            aria-hidden
                          />
                        ) : (
                          <Folder
                            size={14}
                            className="text-text-muted shrink-0"
                            aria-hidden
                          />
                        )}
                        <span className="truncate">{d.name}</span>
                        {isDropOver && (
                          <span className="ml-auto text-[10px] font-semibold uppercase tracking-wide text-accent">
                            {t("uploads.folder.dropHere", {
                              defaultValue: "Soltar aquí",
                            })}
                          </span>
                        )}
                      </button>
                      {!isDropOver && (
                        <RowActions
                          onRename={() => beginRename(d.path, d.name, true)}
                          onDelete={() => handleDelete(d.path, d.name, true)}
                          renameLabel={t("uploads.folder.renameAction", {
                            defaultValue: "Renombrar",
                          })}
                          deleteLabel={t("uploads.folder.deleteAction", {
                            defaultValue: "Eliminar",
                          })}
                        />
                      )}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}

        {/* Ficheros existentes en la carpeta — read-only. El operador
            los ve para saber "ya está aquí, no lo vuelvo a subir" sin
            tener que ir al catálogo. Sólo subdirs son clicables /
            drop targets; los ficheros no hacen nada al hover. */}
        {data && data.files && data.files.length > 0 && (
          <ul className="border-t border-border/40 py-1">
            {data.files.map((f) => {
              const fullPath = path ? `${path}/${f.name}` : f.name;
              const isRenaming = renameTarget?.fullPath === fullPath;
              return (
                <li
                  key={f.name}
                  className="group/row flex items-center gap-2 px-3 py-1.5 text-sm text-text-muted hover:bg-bg-hover/40"
                >
                  {isRenaming ? (
                    <RenameRow
                      icon={
                        <FileVideo
                          size={13}
                          className="shrink-0 opacity-60"
                          aria-hidden
                        />
                      }
                      value={renameTarget!.newName}
                      onChange={(v) =>
                        setRenameTarget({ ...renameTarget!, newName: v })
                      }
                      onConfirm={confirmRename}
                      onCancel={cancelRename}
                      isPending={renameEntry.isPending}
                      inline
                    />
                  ) : (
                    <>
                      <FileVideo
                        size={13}
                        className="shrink-0 opacity-60"
                        aria-hidden
                      />
                      <span className="truncate flex-1">{f.name}</span>
                      <span className="text-xs shrink-0 opacity-70">
                        {formatFileSize(f.size)}
                      </span>
                      <RowActions
                        onRename={() => beginRename(fullPath, f.name, false)}
                        onDelete={() => handleDelete(fullPath, f.name, false)}
                        renameLabel={t("uploads.folder.renameAction", {
                          defaultValue: "Renombrar",
                        })}
                        deleteLabel={t("uploads.folder.deleteAction", {
                          defaultValue: "Eliminar",
                        })}
                      />
                    </>
                  )}
                </li>
              );
            })}
          </ul>
        )}

        {actionError && (
          <p className="px-3 py-2 text-xs text-red-400" role="alert">
            {actionError}
          </p>
        )}

        {/* Empty-state también es drop target: si la carpeta no tiene
            subdirs, el operador puede arrastrar al CUERPO del browser y
            aterriza en el path actual. */}
        {data && data.directories.length === 0 && dragSupported && (
          <div
            onDragOver={(e) => handleFolderDragOver(e, path)}
            onDragLeave={handleFolderDragLeave}
            onDrop={(e) => handleFolderDrop(e, path)}
            className={[
              "mx-3 my-2 rounded-md border-2 border-dashed py-4 text-center text-xs transition-all",
              dropTarget === path
                ? "border-accent bg-accent/10 text-accent"
                : "border-border/60 text-text-muted",
            ].join(" ")}
          >
            {dropTarget === path
              ? t("uploads.folder.dropHere", { defaultValue: "Soltar aquí" })
              : t("uploads.folder.dropHint", {
                  defaultValue: "Suelta ficheros aquí para subir a esta carpeta",
                })}
          </div>
        )}
      </div>

      {/* New folder */}
      <footer className="border-t border-border bg-bg-base px-3 py-2">
        {showNewFolder ? (
          <form onSubmit={handleCreateFolder} className="flex gap-2">
            <Input
              autoFocus
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder={t("uploads.folder.newFolderPlaceholder", {
                defaultValue: "Nombre de la carpeta",
              })}
              className="flex-1 text-sm"
            />
            <Button
              type="submit"
              size="sm"
              isLoading={createFolder.isPending}
              disabled={!newFolderName.trim()}
            >
              {t("common.create", { defaultValue: "Crear" })}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => {
                setShowNewFolder(false);
                setNewFolderName("");
                setFolderError(null);
              }}
            >
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
          </form>
        ) : (
          <button
            type="button"
            onClick={() => setShowNewFolder(true)}
            className="flex items-center gap-1.5 text-sm text-text-secondary hover:text-text-primary"
          >
            <FolderPlus size={14} aria-hidden />
            {t("uploads.folder.newFolderCta", { defaultValue: "Nueva carpeta" })}
          </button>
        )}
        {folderError && (
          <p className="mt-1 text-xs text-red-400" role="alert">
            {folderError}
          </p>
        )}
      </footer>
    </section>
  );
}

// ─── RowActions ──────────────────────────────────────────────────────
//
// Iconos secundarios (rename + delete) que aparecen al hacer hover en
// una fila. group-hover:opacity-100 los esconde por defecto para que
// la fila normal se vea limpia — el operador descubre que existen al
// pasar el ratón. Pequeños + accent on hover de cada uno; sin
// confirmaciones visuales aquí — la confirmación del delete vive en
// el handler con confirm(). Mismo lenguaje visual que el resto del
// proyecto (kebab menu admin).
function RowActions({
  onRename,
  onDelete,
  renameLabel,
  deleteLabel,
}: {
  onRename: () => void;
  onDelete: () => void;
  renameLabel: string;
  deleteLabel: string;
}) {
  return (
    <div className="flex items-center gap-0.5 pr-2 opacity-0 group-hover/row:opacity-100 focus-within:opacity-100 transition-opacity">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onRename();
        }}
        aria-label={renameLabel}
        title={renameLabel}
        className="rounded p-1 text-text-muted hover:bg-bg-base hover:text-accent transition-colors"
      >
        <Pencil size={12} aria-hidden />
      </button>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onDelete();
        }}
        aria-label={deleteLabel}
        title={deleteLabel}
        className="rounded p-1 text-text-muted hover:bg-bg-base hover:text-red-400 transition-colors"
      >
        <Trash2 size={12} aria-hidden />
      </button>
    </div>
  );
}

// ─── RenameRow ───────────────────────────────────────────────────────
//
// Reemplaza la fila normal cuando se está renombrando. autoFocus +
// select all para que el operador pueda escribir el nombre nuevo
// directamente o usar Enter/Esc para confirmar/cancelar sin click.
function RenameRow({
  icon,
  value,
  onChange,
  onConfirm,
  onCancel,
  isPending,
  inline,
}: {
  icon: React.ReactNode;
  value: string;
  onChange: (v: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
  isPending: boolean;
  inline?: boolean;
}) {
  return (
    <div
      className={[
        "flex items-center gap-2",
        inline ? "" : "px-3 py-1.5",
      ].join(" ")}
    >
      {icon}
      <input
        autoFocus
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onConfirm();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          }
        }}
        onFocus={(e) => {
          // Select all para que escribir reemplace el nombre actual
          // — es lo que el operador típicamente quiere.
          e.target.select();
        }}
        className="flex-1 rounded-sm border border-accent/60 bg-bg-base px-1.5 py-0.5 text-sm text-text-primary outline-none focus:border-accent"
      />
      <button
        type="button"
        onClick={onConfirm}
        disabled={isPending || !value.trim()}
        aria-label="confirm"
        className="rounded p-1 text-green-500 hover:bg-bg-base disabled:opacity-30"
      >
        <Check size={13} aria-hidden />
      </button>
      <button
        type="button"
        onClick={onCancel}
        aria-label="cancel"
        className="rounded p-1 text-text-muted hover:bg-bg-base"
      >
        <X size={13} aria-hidden />
      </button>
    </div>
  );
}

// formatFileSize convierte bytes a la representación binaria humana
// más corta. Inline porque sólo lo usa este componente — la otra
// instancia (humanBytes en Uploads.tsx) tiene la misma lógica pero
// con mayor verbosidad de comentarios. Cuando exista un tercer
// llamante, extraer a utils.
function formatFileSize(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let val = n / 1024;
  let i = 0;
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024;
    i++;
  }
  return `${val.toFixed(1)} ${units[i]}`;
}
