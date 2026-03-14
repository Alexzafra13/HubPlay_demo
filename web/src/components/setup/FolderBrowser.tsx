import { useState, useCallback } from "react";
import type { FC } from "react";
import { useBrowseDirectories } from "@/api/hooks";
import { Modal } from "@/components/common/Modal";
import { Button } from "@/components/common/Button";
import { Spinner } from "@/components/common/Spinner";

interface FolderBrowserProps {
  isOpen: boolean;
  onClose: () => void;
  onSelect: (path: string) => void;
}

const FolderBrowser: FC<FolderBrowserProps> = ({ isOpen, onClose, onSelect }) => {
  const [currentPath, setCurrentPath] = useState<string | undefined>(undefined);

  const { data, isLoading, isError, error } = useBrowseDirectories(currentPath, {
    enabled: isOpen,
  });

  const handleNavigate = useCallback((path: string) => {
    setCurrentPath(path);
  }, []);

  const handleGoUp = useCallback(() => {
    if (data?.parent) {
      setCurrentPath(data.parent);
    }
  }, [data?.parent]);

  const handleSelect = useCallback(() => {
    if (data?.current) {
      onSelect(data.current);
      onClose();
    }
  }, [data?.current, onSelect, onClose]);

  // Build breadcrumb segments from current path
  const breadcrumbs = data?.current
    ? data.current.split("/").filter(Boolean)
    : [];

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Browse Folders" size="lg">
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
                    className="h-3 w-3 shrink-0 text-text-muted"
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

        {/* Directory listing */}
        <div className="min-h-[280px] max-h-[400px] overflow-y-auto rounded-[--radius-md] border border-border bg-bg-base">
          {isLoading && (
            <div className="flex items-center justify-center py-16">
              <Spinner size="md" />
            </div>
          )}

          {isError && (
            <div className="flex flex-col items-center justify-center gap-2 py-16 px-4">
              <svg
                className="h-6 w-6 text-error"
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

          {!isLoading && !isError && data && (
            <div className="divide-y divide-border">
              {/* Go up entry */}
              {data.parent && data.current !== "/" && (
                <button
                  onClick={handleGoUp}
                  className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-text-secondary hover:bg-bg-elevated transition-colors cursor-pointer"
                >
                  <svg
                    className="h-4 w-4 shrink-0"
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
              {data.directories.length === 0 && (
                <div className="px-4 py-8 text-center text-sm text-text-muted">
                  No subdirectories found
                </div>
              )}

              {data.directories.map((dir) => (
                <button
                  key={dir.path}
                  onClick={() => handleNavigate(dir.path)}
                  className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-text-primary hover:bg-bg-elevated transition-colors cursor-pointer"
                >
                  <svg
                    className="h-4 w-4 shrink-0 text-accent"
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

        {/* Current path display + Select button */}
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0 flex-1">
            <p className="text-xs text-text-muted">Selected path:</p>
            <p className="truncate text-sm font-mono text-text-primary">
              {data?.current ?? "..."}
            </p>
          </div>
          <Button
            onClick={handleSelect}
            disabled={!data?.current}
          >
            Select this folder
          </Button>
        </div>
      </div>
    </Modal>
  );
};

export { FolderBrowser };
export type { FolderBrowserProps };
