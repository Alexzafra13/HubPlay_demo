import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Folder,
  FolderPlus,
  ChevronRight,
  ChevronUp,
  Home,
  Library as LibraryIcon,
  Loader2,
} from "lucide-react";

import { useUploadBrowse, useCreateUploadFolder } from "@/api/hooks";
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
}

export function FolderBrowser({
  libraries,
  libraryID,
  path,
  onChange,
}: FolderBrowserProps) {
  const { t } = useTranslation();
  const { data, isLoading, error } = useUploadBrowse(libraryID, path, !!libraryID);
  const createFolder = useCreateUploadFolder();

  const [showNewFolder, setShowNewFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [folderError, setFolderError] = useState<string | null>(null);

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

      {/* Breadcrumb + back */}
      <div className="flex items-center gap-1 border-b border-border bg-bg-base px-3 py-2 text-sm overflow-x-auto">
        <button
          type="button"
          onClick={() => enterFolder("")}
          aria-label={t("uploads.folder.root", { defaultValue: "Raíz" })}
          className="flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 hover:bg-bg-hover"
        >
          <Home size={13} aria-hidden />
        </button>
        {breadcrumbs.map((bc) => (
          <span key={bc.path} className="flex shrink-0 items-center gap-1">
            <ChevronRight size={12} className="text-text-muted" aria-hidden />
            <button
              type="button"
              onClick={() => enterFolder(bc.path)}
              className="rounded px-1.5 py-0.5 hover:bg-bg-hover truncate max-w-[160px]"
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

        {data && data.directories.length === 0 && !isLoading && (
          <p className="px-3 py-3 text-xs text-text-muted italic">
            {t("uploads.folder.empty", {
              defaultValue: "Esta carpeta no tiene subcarpetas. Sube aquí o crea una nueva.",
            })}
          </p>
        )}

        {data && data.directories.length > 0 && (
          <ul className="py-1">
            {data.directories.map((d) => (
              <li key={d.path}>
                <button
                  type="button"
                  onClick={() => enterFolder(d.path)}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-bg-hover"
                >
                  <Folder
                    size={14}
                    className="text-text-muted shrink-0"
                    aria-hidden
                  />
                  <span className="truncate">{d.name}</span>
                </button>
              </li>
            ))}
          </ul>
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
