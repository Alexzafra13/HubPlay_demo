// FolderBrowser — host filesystem directory picker.
//
// Shipped in two shapes:
//
//   - <FolderBrowserContent /> — pure content (breadcrumb, listing,
//     footer with Select). No Modal, no portal, no scroll lock. Drop
//     it into any container; flows that already own a parent dialog
//     should use this and render it as a wizard step rather than
//     stacking another modal on top.
//
//   - <FolderBrowser /> — convenience wrapper that puts <Content />
//     inside a <Modal>. For standalone callers that genuinely want a
//     modal (the first-run setup wizard's "Pick a folder" prompt is
//     the only one in-tree).
//
// The inline + standalone split is what kills the "modal-in-modal
// orphan" bug. Admin flows that previously rendered <FolderBrowser>
// as a sibling of their own <Modal> now use <FolderBrowserContent>
// inside their own modal as a step. There's no second portal, no
// second backdrop, no second scroll lock — nothing that can outlive
// the parent.

import { useState } from "react";
import type { FC } from "react";
import { useBrowseDirectories, useBrowseLibraryDirectories } from "@/api/hooks";
import { Modal } from "@/components/common/Modal";
import { Button } from "@/components/common/Button";
import { Spinner } from "@/components/common/Spinner";

interface FolderBrowserContentProps {
  /** Called with the absolute path of the folder the user picked. */
  onSelect: (path: string) => void;
  /** Optional cancel button shown next to "Select this folder". */
  onCancel?: () => void;
  /** Use the authenticated admin browse endpoint instead of the setup one */
  useAdmin?: boolean;
}

const FolderBrowserContent: FC<FolderBrowserContentProps> = ({
  onSelect,
  onCancel,
  useAdmin,
}) => {
  const [currentPath, setCurrentPath] = useState<string | undefined>(undefined);

  const setupBrowse = useBrowseDirectories(currentPath, {
    enabled: !useAdmin,
  });
  const adminBrowse = useBrowseLibraryDirectories(currentPath, {
    enabled: !!useAdmin,
  });

  const { data, isLoading, isError, error, isFetching } = useAdmin
    ? adminBrowse
    : setupBrowse;
  // Distinguish "first time loading at all" from "swapping a fetched
  // listing for a different path". `isLoading` is true only on the
  // very first fetch (no cached data exists); `isFetching` flips on
  // every subsequent navigation.
  const isNavigating = !isLoading && isFetching;
  // Whether the placeholder data is stale relative to where the user
  // *thinks* they are. Once `data.current` matches `currentPath` the
  // new listing has landed; until then the breadcrumb / list still
  // belong to the previous path. Used to swap the dim-previous-list
  // visual for an explicit "Going to X…" spinner so a slow fetch
  // doesn't read as "the tap was lost".
  const targetPath = currentPath ?? "/";
  const isStalePlaceholder =
    isNavigating && data?.current !== undefined && data.current !== targetPath;

  const handleNavigate = (path: string) => setCurrentPath(path);

  const handleGoUp = () => {
    if (data?.parent) setCurrentPath(data.parent);
  };

  const handleSelect = () => {
    if (data?.current) onSelect(data.current);
  };

  // Build breadcrumb segments from current path
  const breadcrumbs = data?.current
    ? data.current.split("/").filter(Boolean)
    : [];

  return (
    <div className="flex flex-col gap-4">
      {/* Breadcrumb */}
      {data?.current && (
        <div className="flex items-center gap-1 overflow-x-auto text-sm">
          <button
            onClick={() => setCurrentPath("/")}
            className="shrink-0 rounded px-1.5 py-0.5 text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors cursor-pointer"
          >
            /
          </button>
          {breadcrumbs.map((segment, index) => {
            const segmentPath = "/" + breadcrumbs.slice(0, index + 1).join("/");
            const isLast = index === breadcrumbs.length - 1;

            return (
              <div key={segmentPath} className="flex items-center gap-1">
                <svg
                  className="size-3 shrink-0 text-text-muted"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                >
                  <path
                    fillRule="evenodd"
                    d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z"
                    clipRule="evenodd"
                  />
                </svg>
                <button
                  onClick={() => !isLast && setCurrentPath(segmentPath)}
                  disabled={isLast}
                  className={[
                    "shrink-0 rounded px-1.5 py-0.5 transition-colors",
                    isLast
                      ? "text-text-primary font-medium"
                      : "text-text-secondary hover:text-text-primary hover:bg-bg-elevated cursor-pointer",
                  ].join(" ")}
                >
                  {segment}
                </button>
              </div>
            );
          })}
        </div>
      )}

      {/* Directory listing.
          Wrapper carries a thin top-progress strip while a navigation
          request is in flight so the user sees motion even when the
          previous listing is still painted underneath. The cold-start
          spinner only fires when there's no cached listing at all. */}
      <div className="relative min-h-[280px] max-h-[400px] overflow-y-auto rounded-[--radius-md] border border-border bg-bg-base">
        {isNavigating && (
          <div
            className="absolute inset-x-0 top-0 z-10 h-0.5 overflow-hidden bg-bg-elevated"
            aria-hidden="true"
          >
            <div className="h-full w-1/3 animate-[shimmer_1s_linear_infinite] bg-accent" />
          </div>
        )}

        {(isLoading || isStalePlaceholder) && (
          <div className="flex flex-col items-center justify-center gap-3 py-16">
            <Spinner size="md" />
            <p className="text-xs text-text-muted">
              {targetPath !== "/" ? `Cargando ${targetPath}…` : "Cargando…"}
            </p>
          </div>
        )}

        {isError && (
          <div className="flex flex-col items-center justify-center gap-2 py-16 px-4">
            <svg
              className="size-6 text-error"
              viewBox="0 0 20 20"
              fill="currentColor"
            >
              <path
                fillRule="evenodd"
                d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-5a.75.75 0 01.75.75v4.5a.75.75 0 01-1.5 0v-4.5A.75.75 0 0110 5zm0 10a1 1 0 100-2 1 1 0 000 2z"
                clipRule="evenodd"
              />
            </svg>
            <p className="text-sm text-error text-center">
              {error instanceof Error ? error.message : "Failed to browse directories"}
            </p>
          </div>
        )}

        {!isLoading && !isError && !isStalePlaceholder && data && (
          <div className="divide-y divide-border">
            {/* Go up entry */}
            {data.parent && data.current !== "/" && (
              <button
                onClick={handleGoUp}
                className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-text-secondary hover:bg-bg-elevated transition-colors cursor-pointer"
              >
                <svg
                  className="size-4 shrink-0"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                >
                  <path
                    fillRule="evenodd"
                    d="M17 10a.75.75 0 01-.75.75H5.612l4.158 3.96a.75.75 0 11-1.04 1.08l-5.5-5.25a.75.75 0 010-1.08l5.5-5.25a.75.75 0 011.04 1.08L5.612 9.25H16.25A.75.75 0 0117 10z"
                    clipRule="evenodd"
                  />
                </svg>
                <span>..</span>
              </button>
            )}

            {/* Directories */}
            {(!data.directories || data.directories.length === 0) && (
              <div className="px-4 py-8 text-center text-sm text-text-muted">
                No subdirectories found
              </div>
            )}

            {(data.directories ?? []).map((dir) => (
              <button
                key={dir.path}
                onClick={() => handleNavigate(dir.path)}
                className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-text-primary hover:bg-bg-elevated transition-colors cursor-pointer"
              >
                <svg
                  className="size-4 shrink-0 text-accent"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                >
                  <path d="M3.75 3A1.75 1.75 0 002 4.75v3.26a3.235 3.235 0 011.75-.51h12.5c.644 0 1.245.188 1.75.51V6.75A1.75 1.75 0 0016.25 5h-4.836a.25.25 0 01-.177-.073L9.823 3.513A1.75 1.75 0 008.586 3H3.75zM3.75 9A1.75 1.75 0 002 10.75v4.5c0 .966.784 1.75 1.75 1.75h12.5A1.75 1.75 0 0018 15.25v-4.5A1.75 1.75 0 0016.25 9H3.75z" />
                </svg>
                <span className="truncate">{dir.name}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Current path display + actions */}
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0 flex-1">
          <p className="text-xs text-text-muted">Selected path:</p>
          <p className="truncate text-sm font-mono text-text-primary">
            {data?.current ?? "..."}
          </p>
        </div>
        <div className="flex gap-2">
          {onCancel && (
            <Button variant="secondary" onClick={onCancel}>
              Cancelar
            </Button>
          )}
          <Button onClick={handleSelect} disabled={!data?.current}>
            Select this folder
          </Button>
        </div>
      </div>
    </div>
  );
};

interface FolderBrowserProps {
  isOpen: boolean;
  onClose: () => void;
  onSelect: (path: string) => void;
  /** Use the authenticated admin browse endpoint instead of the setup one */
  useAdmin?: boolean;
}

const FolderBrowser: FC<FolderBrowserProps> = ({
  isOpen,
  onClose,
  onSelect,
  useAdmin,
}) => {
  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Browse Folders" size="lg">
      <FolderBrowserContent
        useAdmin={useAdmin}
        onSelect={(path) => {
          onSelect(path);
          onClose();
        }}
        onCancel={onClose}
      />
    </Modal>
  );
};

export { FolderBrowser, FolderBrowserContent };
