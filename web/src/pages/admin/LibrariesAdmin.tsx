import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { useDeleteLibrary, useLibraries } from "@/api/hooks";
import { Button, EmptyState, Modal, Skeleton } from "@/components/common";
import type { ContentType, Library } from "@/api/types";
import { LIBRARY_SECTIONS } from "./librariesAdmin/constants";
import { SectionChevron } from "./librariesAdmin/SectionChevron";
import {
  LibraryCard,
  type RefreshMessage,
} from "./librariesAdmin/LibraryCard";
import { LibraryEditModal } from "./librariesAdmin/LibraryEditModal";

export default function LibrariesAdmin() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: libraries, isLoading, error } = useLibraries();
  const deleteLibrary = useDeleteLibrary();

  // Page-level UI state. "New" lives at /admin/libraries/new now
  // (URL = state), so the page only owns row-scoped overlays:
  // which row is being edited (target → Sheet) or deleted (target →
  // confirm). The refresh banner is global because the user can
  // trigger several refreshes in a row.
  const [editTarget, setEditTarget] = useState<Library | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Library | null>(null);
  const [refreshMessage, setRefreshMessage] = useState<RefreshMessage | null>(null);
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
      <div className="flex flex-col gap-6">
        <div className="flex items-center justify-between">
          <Skeleton variant="text" width={140} height={20} />
          <Skeleton variant="rectangular" width={120} height={36} />
        </div>
        {LIBRARY_SECTIONS.slice(0, 2).map((section) => (
          <div key={section.type} className="flex flex-col gap-3">
            <Skeleton variant="text" width={180} height={18} />
            <ul className="flex flex-col gap-2">
              {Array.from({ length: 2 }, (_, i) => (
                <li
                  key={i}
                  className="rounded-[--radius-lg] border border-border bg-bg-card overflow-hidden"
                >
                  <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:gap-4">
                    <div className="flex flex-1 flex-col gap-2">
                      <Skeleton variant="text" width="40%" />
                      <Skeleton variant="text" width="65%" />
                    </div>
                    <div className="flex gap-1">
                      <Skeleton variant="rectangular" width={64} height={28} />
                      <Skeleton variant="rectangular" width={64} height={28} />
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <EmptyState
        title={t("admin.libraries.failedToLoad")}
        description={t("common.loadErrorHint")}
      />
    );
  }

  const totalLibraries = libraries?.length ?? 0;

  return (
    <div className="flex flex-col gap-5">
      {/* Header — title + meta on the left, primary action on the
          right. Tabular nums on the count so it doesn't twitch as
          libraries are added/removed. */}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="text-[19px] font-semibold tracking-tight text-text-primary">
            {t("admin.libraries.title")}
          </h2>
          <p className="mt-0.5 text-[12px] text-text-muted tabular-nums">
            {totalLibraries === 0
              ? t("admin.libraries.totalNone", { defaultValue: "Sin bibliotecas" })
              : t("admin.libraries.total", {
                  defaultValue: "{{count}} bibliotecas",
                  count: totalLibraries,
                })}
          </p>
        </div>
        <Button className="shrink-0" onClick={() => navigate("/admin/libraries/new")}>
          {t("admin.libraries.addLibrary")}
        </Button>
      </div>

      {refreshMessage && (
        <div
          className={[
            "rounded-[--radius-md] border px-3 py-2 text-[13px]",
            refreshMessage.type === "success"
              ? "border-success/30 bg-success/10 text-success"
              : "border-error/30 bg-error/10 text-error",
          ].join(" ")}
        >
          {refreshMessage.text}
        </div>
      )}

      {libraries && libraries.length > 0 ? (
        <div className="flex flex-col gap-3">
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
                    "flex items-center gap-3 px-3 py-2 rounded-[--radius-md] border text-left transition-colors",
                    headerClass,
                    isOpen ? "rounded-b-none" : "",
                  ].join(" ")}
                >
                  <span className={textClass}>
                    <SectionChevron open={isOpen} />
                  </span>
                  <span
                    aria-hidden
                    className={["h-1.5 w-1.5 rounded-full", dotClass].join(" ")}
                  />
                  <span
                    className={[
                      "text-[11px] font-semibold tracking-[0.08em] uppercase",
                      textClass,
                    ].join(" ")}
                  >
                    {t(labelKey)}
                  </span>
                  <span className="text-[11px] text-text-muted tabular-nums">
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
            <Button onClick={() => navigate("/admin/libraries/new")}>
              {t("admin.libraries.addLibrary")}
            </Button>
          }
        />
      )}

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
            <Trans
              i18nKey="admin.libraries.deleteConfirm"
              values={{ name: deleteTarget?.name ?? "" }}
              components={{ strong: <strong className="text-text-primary" /> }}
            />
          </p>

          {deleteLibrary.error && (
            <p className="text-xs text-error">{deleteLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-2">
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
