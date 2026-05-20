// LibraryDetailPage — per-library management surface.
//
// Why this page exists:
//   The livetv admin panel (4 tabs: sources / sin guía / unhealthy /
//   schedule) used to render INSIDE every LibraryCard on the libraries
//   list. With two livetv libraries the page became two stacked
//   sub-apps with their own tab bars, scroll regions and badge
//   counts. On mobile it was unusable. On desktop it broke the
//   "list = scannable rows" mental model that every Plex/Jellyfin
//   admin relies on.
//
// The fix is structural: each livetv library gets its OWN page,
// reachable via the "Gestionar" button on its row. The list goes
// back to being a list. The detail page is where the panel breathes
// — full-width tabs, no competition from siblings.
//
// Non-livetv libraries don't need this page (their card has every
// action they need). If the user navigates here directly with a
// non-livetv id we show a minimal info view instead of a 404, so
// a deep link from notes / bookmarks doesn't dead-end.

import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router";
import { useLibrary } from "@/api/hooks";
import { Spinner, EmptyState, Button, Badge } from "@/components/common";
import { LivetvAdminPanel } from "@/components/admin/LivetvAdminPanel";

export default function LibraryDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const { data: library, isLoading, error } = useLibrary(id ?? "");

  function back() {
    navigate("/admin/libraries");
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Spinner size="md" />
      </div>
    );
  }

  if (error || !library) {
    return (
      <EmptyState
        title={t("admin.libraries.failedToLoad")}
        description={t("common.loadErrorHint")}
        action={
          <Button variant="secondary" onClick={back}>
            {t("common.back", { defaultValue: "Volver" })}
          </Button>
        }
      />
    );
  }

  const isLivetv = library.content_type === "livetv";

  return (
    <div className="flex flex-col gap-6">
      {/* Page header — back chevron, breadcrumb-style title, meta
          and primary actions. Same shape as LibraryNewPage so the
          admin reads as one consistent product. */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
        <div className="flex items-start gap-3 min-w-0 flex-1">
          <button
            type="button"
            onClick={back}
            className="mt-0.5 -ml-1 p-1.5 rounded-[--radius-sm] text-text-muted hover:text-text-primary hover:bg-bg-elevated transition-colors"
            aria-label="Back"
          >
            <svg
              className="size-4"
              viewBox="0 0 20 20"
              fill="none"
              stroke="currentColor"
              strokeWidth={1.5}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12.5 15l-5-5 5-5"
              />
            </svg>
          </button>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 text-[12px] text-text-muted">
              <button
                type="button"
                onClick={back}
                className="hover:text-text-secondary transition-colors"
              >
                {t("admin.libraries.title")}
              </button>
              <span aria-hidden>/</span>
              <span className="font-medium text-text-secondary truncate">
                {library.name}
              </span>
            </div>
            <h2 className="mt-1 text-[19px] font-semibold tracking-tight text-text-primary leading-tight truncate">
              {library.name}
            </h2>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[12px] text-text-muted tabular-nums">
              <Badge variant="default">{library.content_type}</Badge>
              {library.item_count != null && (
                <span>
                  {t("admin.libraries.itemCount", {
                    defaultValue: "{{count}} elementos",
                    count: library.item_count,
                  })}
                </span>
              )}
              {(library.paths ?? []).length > 0 && (
                <span className="font-mono text-[11px] truncate">
                  {(library.paths ?? [])[0]}
                </span>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Body */}
      {isLivetv ? (
        <div className="rounded-[--radius-lg] border border-border bg-bg-card p-4 sm:p-5">
          <LivetvAdminPanel
            libraryId={library.id}
            totalChannels={library.item_count ?? 0}
          />
        </div>
      ) : (
        <EmptyState
          title={t("admin.libraries.detailNotAvailable", {
            defaultValue: "No hay configuración avanzada para este tipo",
          })}
          description={t("admin.libraries.detailNotAvailableHint", {
            defaultValue:
              "Las acciones diarias (escanear, actualizar metadatos / imágenes, editar) están en la lista. Esta vista de detalle existe sobre todo para bibliotecas en directo.",
          })}
          action={
            <Button variant="secondary" onClick={back}>
              {t("common.back", { defaultValue: "Volver" })}
            </Button>
          }
        />
      )}
    </div>
  );
}
