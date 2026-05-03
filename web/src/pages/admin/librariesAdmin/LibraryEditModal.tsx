// LibraryEditModal — edits an existing library in a side sheet.
//
// Shape: target-driven, no separate isOpen flag. When the parent
// has a Library to edit it passes it in; null closes.
//
// Lives in a Sheet, not a Modal, because editing a row is a
// contextual action — the user wants to keep the row list visible
// while they tweak. The folder picker is still a step inside the
// SAME sheet (view: 'form' | 'browse') so a leaked overlay remains
// structurally impossible.

import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Button, Input, Sheet } from "@/components/common";
import { FolderBrowserContent } from "@/components/setup/FolderBrowser";
import { useUpdateLibrary, usePrefetchBrowseLibraryDirectories } from "@/api/hooks";
import type { Library } from "@/api/types";
import { LanguageMultiSelect } from "./LanguageMultiSelect";
import { PreflightButton } from "./PreflightButton";
import { TLSInsecureToggle } from "./TLSInsecureToggle";

interface LibraryEditModalProps {
  target: Library | null;
  onClose: () => void;
}

export function LibraryEditModal({ target, onClose }: LibraryEditModalProps) {
  const { t } = useTranslation();
  const updateLibrary = useUpdateLibrary();

  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [m3uURL, setM3UURL] = useState("");
  const [epgURL, setEPGURL] = useState("");
  const [languageFilter, setLanguageFilter] = useState<string[]>([]);
  const [tlsInsecure, setTLSInsecure] = useState(false);
  const [view, setView] = useState<"form" | "browse">("form");

  // Hydrate from target on each open. `target` is the source of truth;
  // local state mirrors it while the modal is shown.
  useEffect(() => {
    if (!target) {
      setView("form");
      return;
    }
    setName(target.name);
    setPath((target.paths ?? [])[0] ?? "");
    setM3UURL(target.m3u_url ?? "");
    setEPGURL(target.epg_url ?? "");
    setLanguageFilter(target.language_filter ?? []);
    setTLSInsecure(target.tls_insecure ?? false);
  }, [target]);

  const prefetchBrowse = usePrefetchBrowseLibraryDirectories();
  useEffect(() => {
    if (!target) return;
    void prefetchBrowse();
  }, [target, prefetchBrowse]);

  function submit() {
    if (!target || !name.trim()) return;

    if (target.content_type === "livetv") {
      if (!m3uURL.trim()) return;
      updateLibrary.mutate(
        {
          id: target.id,
          data: {
            name: name.trim(),
            m3u_url: m3uURL.trim(),
            epg_url: epgURL.trim(),
            language_filter: languageFilter,
            tls_insecure: tlsInsecure,
          },
        },
        { onSuccess: onClose },
      );
      return;
    }

    if (!path.trim()) return;
    updateLibrary.mutate(
      {
        id: target.id,
        data: { name: name.trim(), paths: [path.trim()] },
      },
      { onSuccess: onClose },
    );
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    submit();
  }

  return (
    <Sheet
      isOpen={target !== null}
      onClose={onClose}
      title={
        view === "browse"
          ? t("admin.libraries.browseFolders")
          : t("admin.libraries.editLibrary")
      }
      description={
        view === "form" && target
          ? `${target.content_type} · ${target.id.slice(0, 8)}`
          : undefined
      }
      size="lg"
      footer={
        view === "form" ? (
          <>
            <Button variant="secondary" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              isLoading={updateLibrary.isPending}
              onClick={submit}
            >
              {t("common.save")}
            </Button>
          </>
        ) : null
      }
    >
      {view === "browse" ? (
        <FolderBrowserContent
          useAdmin
          onSelect={(picked) => {
            setPath(picked);
            setView("form");
          }}
          onCancel={() => setView("form")}
        />
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col gap-5">
          <Input
            label={t("admin.libraries.name")}
            placeholder={t("admin.libraries.namePlaceholder")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />

          {target?.content_type === "livetv" ? (
            <>
              <Input
                label={t("admin.libraries.m3uUrl", { defaultValue: "URL M3U" })}
                placeholder="https://ejemplo.com/playlist.m3u"
                value={m3uURL}
                onChange={(e) => setM3UURL(e.target.value)}
                required
              />
              <div className="flex flex-col gap-1">
                <Input
                  label={t("admin.libraries.epgUrl", { defaultValue: "URL EPG (opcional)" })}
                  placeholder="https://ejemplo.com/epg.xml"
                  value={epgURL}
                  onChange={(e) => setEPGURL(e.target.value)}
                />
                <p className="text-[11px] leading-snug text-text-muted">
                  {t("admin.libraries.epgURLHint", {
                    defaultValue: "Si el M3U trae url-tvg en su cabecera, se auto-detecta.",
                  })}
                </p>
              </div>

              <LanguageMultiSelect
                value={languageFilter}
                onChange={setLanguageFilter}
              />

              <TLSInsecureToggle
                value={tlsInsecure}
                onChange={setTLSInsecure}
              />

              <PreflightButton m3uURL={m3uURL} tlsInsecure={tlsInsecure} />
            </>
          ) : (
            <div className="flex flex-col gap-2">
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
            </div>
          )}

          {updateLibrary.error && (
            <p className="text-xs text-error">{updateLibrary.error.message}</p>
          )}

          {/* Hidden submit button so Enter still submits the form. The
              visible action lives in the sheet footer. */}
          <button type="submit" className="hidden" aria-hidden tabIndex={-1} />
        </form>
      )}
    </Sheet>
  );
}
