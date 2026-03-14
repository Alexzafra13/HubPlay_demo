import { useAuthStore } from "@/store/auth";
import { useLibraries, useScanLibrary } from "@/api/hooks";
import { Badge, Button, Spinner } from "@/components/common";

function scanStatusVariant(status: string) {
  switch (status) {
    case "scanning":
      return "warning" as const;
    case "error":
      return "error" as const;
    default:
      return "success" as const;
  }
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
              {user?.display_name || "—"}
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
            {libraries.map((lib) => (
              <div
                key={lib.id}
                className="rounded-[--radius-lg] border border-border bg-bg-card p-4"
              >
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-text-primary">
                      {lib.name}
                    </span>
                    <Badge>{lib.content_type}</Badge>
                    <Badge variant={scanStatusVariant(lib.scan_status)}>
                      {lib.scan_status}
                    </Badge>
                  </div>
                  {isAdmin && (
                    <Button
                      variant="secondary"
                      size="sm"
                      isLoading={
                        scanLibrary.isPending &&
                        scanLibrary.variables === lib.id
                      }
                      disabled={lib.scan_status === "scanning"}
                      onClick={() => scanLibrary.mutate(lib.id)}
                    >
                      Scan Now
                    </Button>
                  )}
                </div>
                <div className="flex flex-col gap-1">
                  {lib.paths.map((p) => (
                    <div key={p} className="flex items-center gap-2">
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
                      <code className="text-xs text-text-secondary font-mono">
                        {p}
                      </code>
                    </div>
                  ))}
                </div>
                {lib.item_count !== undefined && (
                  <p className="text-xs text-text-muted mt-2">
                    {lib.item_count} items
                  </p>
                )}
              </div>
            ))}
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
