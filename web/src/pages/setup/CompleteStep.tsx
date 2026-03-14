import { useState } from "react";
import { useNavigate } from "react-router";
import { useSetupComplete } from "@/api/hooks";
import { Button } from "@/components/common";
import type { SetupData } from "./SetupWizard";

// ─── Types ───────────────────────────────────────────────────────────────────

interface CompleteStepProps {
  setupData: SetupData;
}

// ─── Summary Row ────────────────────────────────────────────────────────────

function SummaryItem({
  icon,
  label,
  value,
  variant = "default",
}: {
  icon: "check" | "info" | "warning";
  label: string;
  value: string;
  variant?: "default" | "success" | "warning";
}) {
  const iconColors = {
    default: "text-text-muted bg-bg-elevated",
    success: "text-success bg-success/10",
    warning: "text-warning bg-warning/10",
  };

  return (
    <div className="flex items-center gap-3 py-2">
      <div
        className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full ${iconColors[variant]}`}
      >
        {icon === "check" && (
          <svg className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path
              fillRule="evenodd"
              d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
              clipRule="evenodd"
            />
          </svg>
        )}
        {icon === "info" && (
          <svg className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path
              fillRule="evenodd"
              d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z"
              clipRule="evenodd"
            />
          </svg>
        )}
        {icon === "warning" && (
          <svg className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path
              fillRule="evenodd"
              d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.168 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 6a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 6zm0 9a1 1 0 100-2 1 1 0 000 2z"
              clipRule="evenodd"
            />
          </svg>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-xs text-text-muted">{label}</p>
        <p className="text-sm font-medium text-text-primary truncate">
          {value}
        </p>
      </div>
    </div>
  );
}

// ─── Component ───────────────────────────────────────────────────────────────

export default function CompleteStep({ setupData }: CompleteStepProps) {
  const navigate = useNavigate();
  const completeSetup = useSetupComplete();

  const [scanLibraries, setScanLibraries] = useState(true);
  const [serverError, setServerError] = useState<string | null>(null);

  const libraries = setupData.libraries ?? [];
  const settings = setupData.settings;
  const hasTmdb = Boolean(settings?.tmdbApiKey);
  const hasHwAccel = Boolean(settings?.hwAccel);

  function handleFinish() {
    setServerError(null);

    completeSetup.mutate(scanLibraries, {
      onSuccess() {
        navigate("/");
      },
      onError(err) {
        setServerError(
          err.message || "Failed to complete setup. Please try again.",
        );
      },
    });
  }

  return (
    <div>
      {/* Header */}
      <div className="mb-6 text-center">
        <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-full bg-success/10">
          <svg
            className="h-7 w-7 text-success"
            viewBox="0 0 20 20"
            fill="currentColor"
          >
            <path
              fillRule="evenodd"
              d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
              clipRule="evenodd"
            />
          </svg>
        </div>
        <h2 className="text-xl font-semibold text-text-primary">
          You're all set!
        </h2>
        <p className="mt-1 text-sm text-text-secondary">
          Here's a summary of your configuration.
        </p>
      </div>

      {/* Summary card */}
      <div className="rounded-[--radius-md] border border-border bg-bg-surface p-4 mb-6">
        <div className="divide-y divide-border">
          {/* Admin account */}
          <SummaryItem
            icon="check"
            label="Admin Account"
            value={setupData.user?.username ?? "Created"}
            variant="success"
          />

          {/* Libraries */}
          {libraries.length > 0 ? (
            <div className="py-2">
              <div className="flex items-center gap-3">
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-success/10">
                  <svg
                    className="h-3.5 w-3.5 text-success"
                    viewBox="0 0 20 20"
                    fill="currentColor"
                  >
                    <path
                      fillRule="evenodd"
                      d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                      clipRule="evenodd"
                    />
                  </svg>
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-xs text-text-muted">Media Libraries</p>
                  <p className="text-sm font-medium text-text-primary">
                    {libraries.length}{" "}
                    {libraries.length === 1 ? "library" : "libraries"} configured
                  </p>
                </div>
              </div>
              <div className="ml-10 mt-1.5 flex flex-col gap-1">
                {libraries.map((lib, i) => (
                  <div
                    key={i}
                    className="flex items-center gap-2 text-xs text-text-secondary"
                  >
                    <svg
                      className="h-3 w-3 shrink-0 text-accent"
                      viewBox="0 0 20 20"
                      fill="currentColor"
                    >
                      <path d="M3.75 3A1.75 1.75 0 002 4.75v3.26a3.235 3.235 0 011.75-.51h12.5c.644 0 1.245.188 1.75.51V6.75A1.75 1.75 0 0016.25 5h-4.836a.25.25 0 01-.177-.073L9.823 3.513A1.75 1.75 0 008.586 3H3.75zM3.75 9A1.75 1.75 0 002 10.75v4.5c0 .966.784 1.75 1.75 1.75h12.5A1.75 1.75 0 0018 15.25v-4.5A1.75 1.75 0 0016.25 9H3.75z" />
                    </svg>
                    <span className="font-medium">{lib.name}</span>
                    <span className="text-text-muted font-mono truncate">
                      {lib.path}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <SummaryItem
              icon="info"
              label="Media Libraries"
              value="None added — you can add libraries later"
              variant="default"
            />
          )}

          {/* TMDb */}
          <SummaryItem
            icon={hasTmdb ? "check" : "info"}
            label="TMDb Metadata"
            value={hasTmdb ? "API key configured" : "Not configured"}
            variant={hasTmdb ? "success" : "default"}
          />

          {/* Transcoding */}
          <SummaryItem
            icon={hasHwAccel ? "check" : "info"}
            label="Hardware Transcoding"
            value={
              hasHwAccel
                ? `${settings!.hwAccel!.toUpperCase()} acceleration enabled`
                : "Software encoding (default)"
            }
            variant={hasHwAccel ? "success" : "default"}
          />
        </div>
      </div>

      {/* Scan checkbox */}
      {libraries.length > 0 && (
        <label className="mb-6 flex items-center gap-3 rounded-[--radius-md] border border-border bg-bg-surface px-4 py-3 cursor-pointer hover:bg-bg-elevated transition-colors">
          <input
            type="checkbox"
            checked={scanLibraries}
            onChange={(e) => setScanLibraries(e.target.checked)}
            className="h-4 w-4 rounded text-accent focus:ring-accent border-border"
          />
          <div>
            <p className="text-sm font-medium text-text-primary">
              Start scanning libraries now
            </p>
            <p className="text-xs text-text-muted">
              This will begin indexing your media files in the background.
            </p>
          </div>
        </label>
      )}

      {serverError && (
        <p className="mb-4 rounded-[--radius-sm] bg-error/10 px-3 py-2 text-sm text-error">
          {serverError}
        </p>
      )}

      {/* Finish button */}
      <div className="flex justify-center">
        <Button
          size="lg"
          onClick={handleFinish}
          isLoading={completeSetup.isPending}
          className="min-w-[200px]"
        >
          Finish Setup
        </Button>
      </div>
    </div>
  );
}
