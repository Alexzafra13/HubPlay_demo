// LibraryEditModal — the "Edit library" modal.
//
// Driven by `target` rather than a separate isOpen flag: when the
// parent has a Library to edit, the modal opens with that row's values
// preloaded; when target flips back to null, it closes.

import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Button, Input, Modal } from "@/components/common";
import { FolderBrowser } from "@/components/setup/FolderBrowser";
import { useUpdateLibrary } from "@/api/hooks";
import type { Library } from "@/api/types";

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
  const [showBrowse, setShowBrowse] = useState(false);

  // Hydrate from target on each open. `target` is the source of truth;
  // local state mirrors it while the modal is shown.
  useEffect(() => {
    if (!target) return;
    setName(target.name);
    setPath((target.paths ?? [])[0] ?? "");
    setM3UURL(target.m3u_url ?? "");
    setEPGURL(target.epg_url ?? "");
  }, [target]);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!target || !name.trim()) return;

    if (target.content_type === "livetv") {
      if (!m3uURL.trim()) return;
      updateLibrary.mutate(
        {
          id: target.id,
          data: {
            name: name.trim(),
            m3u_url: m3uURL.trim(),
            // Explicit empty string clears; if the admin wants to
            // preserve we still send the trimmed value (which is
            // identical to current).
            epg_url: epgURL.trim(),
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

  return (
    <>
      <Modal
        isOpen={target !== null}
        onClose={onClose}
        title={t("admin.libraries.editLibrary")}
      >
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
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
              <Input
                label={t("admin.libraries.epgUrl", { defaultValue: "URL EPG (opcional)" })}
                placeholder="https://ejemplo.com/epg.xml"
                value={epgURL}
                onChange={(e) => setEPGURL(e.target.value)}
              />
              <p className="-mt-2 text-[11px] text-text-muted">
                {t("admin.libraries.epgURLHint", {
                  defaultValue: "Si el M3U trae url-tvg en su cabecera, se auto-detecta.",
                })}
              </p>
            </>
          ) : (
            <div className="flex gap-2 items-end">
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
                onClick={() => setShowBrowse(true)}
              >
                {t("common.browse")}
              </Button>
            </div>
          )}

          {updateLibrary.error && (
            <p className="text-xs text-error">{updateLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button variant="secondary" type="button" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" isLoading={updateLibrary.isPending}>
              {t("common.save")}
            </Button>
          </div>
        </form>
      </Modal>

      <FolderBrowser
        isOpen={showBrowse}
        onClose={() => setShowBrowse(false)}
        onSelect={(picked) => setPath(picked)}
        useAdmin
      />
    </>
  );
}
