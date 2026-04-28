import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useDeleteLibrary, useLibraries } from "@/api/hooks";
import { Button, EmptyState, Modal, Spinner } from "@/components/common";
import type { ContentType, Library } from "@/api/types";
import { LIBRARY_SECTIONS } from "./librariesAdmin/constants";
import { SectionChevron } from "./librariesAdmin/SectionChevron";
import {
  LibraryCard,
  type RefreshMessage,
} from "./librariesAdmin/LibraryCard";
import { LibraryFormModal } from "./librariesAdmin/LibraryFormModal";
import { LibraryEditModal } from "./librariesAdmin/LibraryEditModal";

export default function LibrariesAdmin() {
  const { t } = useTranslation();
  const { data: libraries, isLoading, error } = useLibraries();
  const deleteLibrary = useDeleteLibrary();

  // Page-level UI state. Per-form / per-row state lives inside the
  // sub-components; everything that crosses component boundaries
  // (which modal is open, which row is being deleted, the global
  // refresh-toast banner) stays here.
  const [showAddModal, setShowAddModal] = useState(false);
  const [editTarget, setEditTarget] = useState<Library | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Library | null>(null);
  const [refreshMessage, setRefreshMessage] = useState<RefreshMessage | null>(null);
  // Sections are open by default; the admin can collapse the ones they
  // aren't actively touching. Set instead of object so the toggle stays
  // a one-liner and order doesn't matter.
  const [collapsedSections, setCollapsedSections] = useState<Set<ContentType>>(
    () => new Set(),
  );

  function toggleSection(type: ContentType) {
    setCollapsedSections((prev) => {
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      return next;
    });
  }

  // Auto-clear refresh message after 4 seconds.
  useEffect(() => {
    if (!refreshMessage) return;
    const timer = setTimeout(() => setRefreshMessage(null), 4000);
    return () => clearTimeout(timer);
  }, [refreshMessage]);

  function handleDelete() {
    if (!deleteTarget) return;
    deleteLibrary.mutate(deleteTarget.id, {
      onSuccess: () => setDeleteTarget(null),
    });
  }

  if (isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <EmptyState
        title={t("admin.libraries.failedToLoad")}
        description={error.message}
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header — wraps on narrow viewports so the "Add library" button
          stays visible instead of being pushed off the right edge. */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="min-w-0 flex-1 text-lg font-semibold text-text-primary">
          {t("admin.libraries.title")}
        </h2>
        <Button className="shrink-0" onClick={() => setShowAddModal(true)}>
          {t("admin.libraries.addLibrary")}
        </Button>
      </div>

      {/* Refresh toast — global because the user might trigger several
          refreshes in a row (M3U then EPG then images), and a banner
          near the action looks the same for any. The 4s auto-clear is
          enforced by the effect above. */}
      {refreshMessage && (
        <div
          className={[
            "rounded-[--radius-md] px-4 py-2 text-sm",
            refreshMessage.type === "success"
              ? "bg-success/10 text-success"
              : "bg-error/10 text-error",
          ].join(" ")}
        >
          {refreshMessage.text}
        </div>
      )}

      {/* Libraries grouped by content type. Each section is a coloured
          collapsible panel — amber for Películas, cyan for Series, red
          for TV en vivo. Click the header to fold a section out of the
          way; useful when one category dominates (a Spanish IPTV admin
          may have 3 livetv libraries and only 1 movies library). Empty
          sections are skipped entirely. */}
      {libraries && libraries.length > 0 ? (
        <div className="flex flex-col gap-4">
          {LIBRARY_SECTIONS.map(({ type, labelKey, headerClass, dotClass, textClass }) => {
            const libs = libraries.filter((l) => l.content_type === type);
            if (libs.length === 0) return null;
            const isOpen = !collapsedSections.has(type);
            return (
              <section key={type} className="flex flex-col">
                <button
                  type="button"
                  onClick={() => toggleSection(type)}
                  aria-expanded={isOpen}
                  className={[
                    "flex items-center gap-3 px-3.5 py-2.5 rounded-[--radius-md] border text-left transition-colors",
                    headerClass,
                    isOpen ? "rounded-b-none" : "",
                  ].join(" ")}
                >
                  <span className={textClass}>
                    <SectionChevron open={isOpen} />
                  </span>
                  <span
                    aria-hidden
                    className={["h-2 w-2 rounded-full", dotClass].join(" ")}
                  />
                  <span
                    className={[
                      "text-[13px] font-semibold tracking-wider uppercase",
                      textClass,
                    ].join(" ")}
                  >
                    {t(labelKey)}
                  </span>
                  <span className="text-xs text-text-muted tabular-nums">
                    {libs.length}
                  </span>
                </button>
                {isOpen && (
                  <ul className="flex flex-col gap-2 p-2 rounded-b-[--radius-md] border border-t-0 border-border bg-bg-base/40">
                    {libs.map((lib) => (
                      <LibraryCard
                        key={lib.id}
                        library={lib}
                        onEdit={setEditTarget}
                        onDelete={setDeleteTarget}
                        onShowMessage={setRefreshMessage}
                      />
                    ))}
                  </ul>
                )}
              </section>
            );
          })}
        </div>
      ) : (
        <EmptyState
          title={t("admin.libraries.noLibraries")}
          description={t("admin.libraries.noLibrariesHint")}
          action={
            <Button onClick={() => setShowAddModal(true)}>
              {t("admin.libraries.addLibrary")}
            </Button>
          }
        />
      )}

      <LibraryFormModal
        isOpen={showAddModal}
        onClose={() => setShowAddModal(false)}
        onCreated={() => setShowAddModal(false)}
      />

      <LibraryEditModal
        target={editTarget}
        onClose={() => setEditTarget(null)}
      />

      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t("admin.libraries.deleteLibrary")}
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            {t("admin.libraries.deleteConfirm", { name: deleteTarget?.name })}
          </p>

          {deleteLibrary.error && (
            <p className="text-xs text-error">{deleteLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setDeleteTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              isLoading={deleteLibrary.isPending}
              onClick={handleDelete}
            >
              {t("common.delete")}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
