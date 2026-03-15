import { useAuthStore } from "@/store/auth";
import { useLibraries, useScanLibrary } from "@/api/hooks";
import type { Library } from "@/api/types";
import { Badge, Button, Spinner } from "@/components/common";

function getPathAccessible(lib: Library, path: string): boolean | undefined {
  const status = lib.path_status?.find((ps) => ps.path === path);
  return status?.accessible;
}

export default function Settings() {
  const { user } = useAuthStore();
  const isAdmin = user?.role === "admin";
  const { data: libraries, isLoading: libsLoading } = useLibraries();
  const scanLibrary = useScanLibrary();

  return (
    <div className="flex flex-col gap-8 px-6 py-8 sm:px-10 max-w-4xl">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        Settings
      </h1>

      {/* Account Info */}
      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">Account</h2>
        <div className="rounded-[--radius-lg] border border-border bg-bg-card divide-y divide-border">
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-text-muted">Username</span>
            <span className="text-sm font-medium text-text-primary">
              {user?.username}
            </span>
          </div>
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-text-muted">Display Name</span>
            <span className="text-sm font-medium text-text-primary">
              {user?.display_name || "\u2014"}
            </span>
          </div>
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-text-muted">Role</span>
            <Badge variant={user?.role === "admin" ? "warning" : "default"}>
              {user?.role}
            </Badge>
          </div>
        </div>
      </section>

      {/* Libraries Overview */}
      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          Media Libraries
        </h2>

        {libsLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : libraries && libraries.length > 0 ? (
          <div className="flex flex-col gap-3">
            {libraries.map((lib) => {
              const hasInaccessiblePaths = lib.path_status?.some(
                (ps) => !ps.accessible,
              );
              return (
                <div
                  key={lib.id}
                  className={`rounded-[--radius-lg] border bg-bg-card p-4 ${
                    hasInaccessiblePaths
                      ? "border-red-500/50"
                      : "border-border"
                  }`}
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-text-primary">
                        {lib.name}
                      </span>
                      <Badge>{lib.content_type}</Badge>
                    </div>
                    {isAdmin && (
                      <Button
                        variant="secondary"
                        size="sm"
                        isLoading={
                          scanLibrary.isPending &&
                          scanLibrary.variables === lib.id
                        }
                        onClick={() => scanLibrary.mutate(lib.id)}
                      >
                        Scan Now
                      </Button>
                    )}
                  </div>

                  <div className="flex flex-col gap-1.5">
                    {lib.paths.map((p) => {
                      const accessible = getPathAccessible(lib, p);
                      return (
                        <div key={p} className="flex items-center gap-2">
                          {accessible === false ? (
                            <svg
                              width="14"
                              height="14"
                              viewBox="0 0 20 20"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="1.5"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              className="text-red-400 flex-shrink-0"
                            >
                              <circle cx="10" cy="10" r="8" />
                              <path d="M10 6v5M10 13.5v.5" />
                            </svg>
                          ) : accessible === true ? (
                            <svg
                              width="14"
                              height="14"
                              viewBox="0 0 20 20"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="1.5"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              className="text-green-400 flex-shrink-0"
                            >
                              <circle cx="10" cy="10" r="8" />
                              <path d="M7 10l2 2 4-4" />
                            </svg>
                          ) : (
                            <svg
                              width="14"
                              height="14"
                              viewBox="0 0 20 20"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="1.5"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              className="text-text-muted flex-shrink-0"
                            >
                              <path d="M2 5a1 1 0 011-1h4l2 2h8a1 1 0 011 1v8a1 1 0 01-1 1H3a1 1 0 01-1-1V5z" />
                            </svg>
                          )}
                          <code
                            className={`text-xs font-mono ${
                              accessible === false
                                ? "text-red-400"
                                : "text-text-secondary"
                            }`}
                          >
                            {p}
                          </code>
                          {accessible === false && (
                            <span className="text-xs text-red-400">
                              Path not found
                            </span>
                          )}
                        </div>
                      );
                    })}
                  </div>

                  {hasInaccessiblePaths && isAdmin && (
                    <div className="mt-3 rounded-md bg-red-500/10 border border-red-500/20 px-3 py-2">
                      <p className="text-xs text-red-400">
                        One or more paths are not accessible. Check that the
                        volume is mounted correctly in docker-compose.yml and
                        that the path matches the container mount point.
                      </p>
                    </div>
                  )}

                  <div className="flex items-center gap-3 mt-3 pt-2 border-t border-border">
                    <span className="text-xs text-text-muted">
                      {lib.item_count ?? 0} items
                    </span>
                    <span className="text-xs text-text-muted">
                      Scan mode: {lib.scan_mode ?? "manual"}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="rounded-[--radius-lg] border border-border bg-bg-card p-6 text-center">
            <p className="text-sm text-text-muted">
              No libraries configured.
              {isAdmin && " Go to Administration to add one."}
            </p>
          </div>
        )}
      </section>
    </div>
  );
}
