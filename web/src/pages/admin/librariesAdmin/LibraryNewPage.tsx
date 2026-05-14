// LibraryNewPage — full-page "create library" form.
//
// Replaces what used to be LibraryFormModal. Promoting Add Library
// from a centred modal to a dedicated route gets us:
//   - URL = state. /admin/libraries/new is bookmarkable, refresh-
//     friendly, the back button works, and a refreshed tab lands
//     back on the same screen instead of an empty list.
//   - Real estate. A long form with livetv source picker, country
//     filter, EPG URL, language filter and TLS toggle never fit
//     comfortably in a max-w-lg modal. The page lays out as two
//     columns on desktop (form + side help) and stacks on mobile.
//   - Cleaner visual hierarchy. Page-level title and back button
//     read like "I'm in a sub-task of admin", which is what's
//     happening — instead of an overlay that interrupts the list.
//
// The folder picker stays as an inline "view: form ↔ browse" step
// so single-portal-per-flow holds. No nested overlays anywhere.

import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { Button, Input } from "@/components/common";
import { FolderBrowserContent } from "@/components/setup/FolderBrowser";
import {
  useCreateLibrary,
  useRefreshM3U,
  usePrefetchBrowseLibraryDirectories,
} from "@/api/hooks";
import type { ContentType } from "@/api/types";
import { LiveTvFormFields } from "./LiveTvFormFields";
import {
  makeInitialLiveTvFormState,
  resolveLiveTvForm,
} from "./liveTvFormState";
import { CONTENT_TYPES } from "./constants";

export default function LibraryNewPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const createLibrary = useCreateLibrary();
  const refreshM3U = useRefreshM3U();

  const [name, setName] = useState("");
  const [type, setType] = useState<ContentType>("movies");
  const [path, setPath] = useState("");
  const [view, setView] = useState<"form" | "browse">("form");
  const [liveState, setLiveState] = useState(makeInitialLiveTvFormState);
  const [validationError, setValidationError] = useState<string | null>(null);

  // Warm the folder-picker cache while the user fills in the form.
  // No-op when already cached.
  const prefetchBrowse = usePrefetchBrowseLibraryDirectories();
  useEffect(() => {
    void prefetchBrowse();
  }, [prefetchBrowse]);

  // Clear the inline validation message whenever the user edits a
  // field — they're showing intent to fix it; nagging the previous
  // error would feel hostile.
  useEffect(() => {
    if (validationError) setValidationError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [name, path, type, liveState]);

  function close() {
    navigate("/admin/libraries");
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();

    if (!name.trim()) {
      setValidationError(
        t("admin.libraries.errors.nameRequired", {
          defaultValue: "El nombre es obligatorio.",
        }),
      );
      return;
    }

    if (type === "livetv") {
      const resolved = resolveLiveTvForm(liveState, t);
      if (!resolved.ok) {
        setValidationError(resolved.error);
        return;
      }
      createLibrary.mutate(
        {
          name: name.trim(),
          content_type: "livetv",
          paths: [],
          ...resolved.payload,
        },
        {
          onSuccess: (lib) => {
            // Auto-trigger the first M3U refresh so the library isn't
            // empty the moment the user lands back on the list.
            refreshM3U.mutate(lib.id);
            navigate("/admin/libraries");
          },
        },
      );
      return;
    }

    if (!path.trim()) {
      setValidationError(
        t("admin.libraries.errors.pathRequired", {
          defaultValue: "Indica al menos una ruta de carpeta.",
        }),
      );
      return;
    }
    createLibrary.mutate(
      { name: name.trim(), content_type: type, paths: [path.trim()] },
      { onSuccess: () => navigate("/admin/libraries") },
    );
  }

  if (view === "browse") {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader
          onBack={() => setView("form")}
          title={t("admin.libraries.browseFolders")}
          description={t("admin.libraries.browseFoldersHint", {
            defaultValue: "Selecciona la carpeta donde están los archivos.",
          })}
        />
        <div className="rounded-[--radius-lg] border border-border bg-bg-card p-4 sm:p-6">
          <FolderBrowserContent
            useAdmin
            onSelect={(picked) => {
              setPath(picked);
              if (!name.trim()) {
                const segments = picked.split("/").filter(Boolean);
                setName(segments[segments.length - 1] ?? "");
              }
              setView("form");
            }}
            onCancel={() => setView("form")}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        onBack={close}
        title={t("admin.libraries.addLibrary")}
        description={t("admin.libraries.addLibraryHint", {
          defaultValue:
            "Crea una nueva colección. Puedes apuntar a una carpeta local o a un servicio en directo.",
        })}
      />

      <form
        onSubmit={handleSubmit}
        className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_280px]"
      >
        <div className="flex flex-col gap-5 rounded-[--radius-lg] border border-border bg-bg-card p-5 sm:p-6">
          <Input
            label={t("admin.libraries.name")}
            placeholder={t("admin.libraries.namePlaceholder")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="content-type"
              className="text-[13px] font-medium tracking-tight text-text-secondary"
            >
              {t("admin.libraries.contentType")}
            </label>
            <select
              id="content-type"
              value={type}
              onChange={(e) => setType(e.target.value as ContentType)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              {CONTENT_TYPES.map((ct) => (
                <option key={ct.value} value={ct.value}>
                  {t(ct.key)}
                </option>
              ))}
            </select>
          </div>

          {type === "livetv" ? (
            <LiveTvFormFields value={liveState} onChange={setLiveState} />
          ) : (
            <div className="flex items-end gap-2">
              <div className="flex-1">
                <Input
                  label={t("admin.libraries.path")}
                  placeholder={t("admin.libraries.pathPlaceholder")}
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  required
                />
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setView("browse")}
              >
                {t("common.browse")}
              </Button>
            </div>
          )}

          {validationError && (
            <div
              role="alert"
              className="rounded-[--radius-md] border border-error/40 bg-error-soft px-3 py-2 text-xs text-error"
            >
              {validationError}
            </div>
          )}

          {createLibrary.error && (
            <p className="text-xs text-error">{createLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-2 pt-1">
            <Button variant="secondary" type="button" onClick={close}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" isLoading={createLibrary.isPending}>
              {t("common.create")}
            </Button>
          </div>
        </div>

        {/* Side help — explains the choices the user is about to
            make so they can pick without context-switching. Plex /
            Jellyfin both have this in their wizard; we put it
            inline so it doesn't gate progress. */}
        <aside className="hidden lg:flex flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-base/40 p-4">
          <p className="text-[11px] font-semibold uppercase tracking-wider text-text-muted">
            {t("admin.libraries.helpHeader", { defaultValue: "Sobre este formulario" })}
          </p>
          <SideTip title={t("admin.libraries.tipNameTitle", { defaultValue: "Nombre" })}>
            {t("admin.libraries.tipNameBody", {
              defaultValue:
                "Aparece en la barra lateral. Cámbialo cuando quieras desde Editar.",
            })}
          </SideTip>
          <SideTip title={t("admin.libraries.tipTypeTitle", { defaultValue: "Tipo de contenido" })}>
            {t("admin.libraries.tipTypeBody", {
              defaultValue:
                "Decide cómo se escanean los archivos (películas, series, música) o si es TV en vivo.",
            })}
          </SideTip>
          {type !== "livetv" ? (
            <SideTip title={t("admin.libraries.tipPathTitle", { defaultValue: "Ruta" })}>
              {t("admin.libraries.tipPathBody", {
                defaultValue:
                  "Apunta a la carpeta dentro del contenedor. Usa el botón Examinar para listarlas.",
              })}
            </SideTip>
          ) : (
            <SideTip title={t("admin.libraries.tipLiveTitle", { defaultValue: "Fuente IPTV" })}>
              {t("admin.libraries.tipLiveBody", {
                defaultValue:
                  "Pública usa listas de iptv-org. Personalizada acepta tu propio M3U.",
              })}
            </SideTip>
          )}
        </aside>
      </form>
    </div>
  );
}

function PageHeader({
  onBack,
  title,
  description,
}: {
  onBack: () => void;
  title: string;
  description?: string;
}) {
  return (
    <div className="flex items-start gap-3">
      <button
        type="button"
        onClick={onBack}
        className="mt-0.5 -ml-1 p-1.5 rounded-[--radius-sm] text-text-muted hover:text-text-primary hover:bg-bg-elevated transition-colors"
        aria-label="Back"
      >
        <svg
          className="h-4 w-4"
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
        >
          <path strokeLinecap="round" strokeLinejoin="round" d="M12.5 15l-5-5 5-5" />
        </svg>
      </button>
      <div className="min-w-0 flex-1">
        <h2 className="text-[19px] font-semibold tracking-tight text-text-primary leading-tight">
          {title}
        </h2>
        {description && (
          <p className="mt-1 text-[13px] text-text-muted">{description}</p>
        )}
      </div>
    </div>
  );
}

function SideTip({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <p className="text-[12px] font-semibold text-text-secondary">{title}</p>
      <p className="text-[12px] leading-snug text-text-muted">{children}</p>
    </div>
  );
}
