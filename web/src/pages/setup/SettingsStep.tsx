import { useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useSystemCapabilities, useSetupSettings } from "@/api/hooks";
import { Button, Input, Spinner, Badge } from "@/components/common";

// ─── Types ───────────────────────────────────────────────────────────────────

interface SettingsData {
  tmdbApiKey?: string;
  hwAccel?: string;
}

interface SettingsStepProps {
  onNext: (data: SettingsData) => void;
  onBack: () => void;
  initialData?: SettingsData;
}

// ─── Component ───────────────────────────────────────────────────────────────

export default function SettingsStep({
  onNext,
  onBack,
  initialData,
}: SettingsStepProps) {
  const { t } = useTranslation();
  const capabilities = useSystemCapabilities();
  const setupSettings = useSetupSettings();

  const [tmdbApiKey, setTmdbApiKey] = useState(initialData?.tmdbApiKey ?? "");
  const [hwAccel, setHwAccel] = useState(initialData?.hwAccel ?? "");
  const [serverError, setServerError] = useState<string | null>(null);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setServerError(null);

    const settings: Record<string, unknown> = {};

    if (tmdbApiKey.trim()) {
      settings.tmdb_api_key = tmdbApiKey.trim();
    }

    if (hwAccel) {
      settings.hw_accel = hwAccel;
    }

    // If nothing to save, skip the API call
    if (Object.keys(settings).length === 0) {
      onNext({ tmdbApiKey: tmdbApiKey.trim() || undefined, hwAccel: hwAccel || undefined });
      return;
    }

    setupSettings.mutate(settings, {
      onSuccess() {
        onNext({
          tmdbApiKey: tmdbApiKey.trim() || undefined,
          hwAccel: hwAccel || undefined,
        });
      },
      onError(err) {
        setServerError(
          err.message || "Failed to save settings. Please try again.",
        );
      },
    });
  }

  function handleSkip() {
    onNext({});
  }

  const ffmpegFound = capabilities.data?.ffmpeg_found ?? false;
  const ffmpegPath = capabilities.data?.ffmpeg_path ?? "";
  const hwAccels = capabilities.data?.hw_accels ?? [];

  return (
    <div>
      <div className="mb-6">
        <h2 className="text-xl font-semibold text-text-primary">
          {t("setup.settings.title")}
        </h2>
        <p className="mt-1 text-sm text-text-secondary">
          {t("setup.settings.description")}
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-6">
        {/* TMDb API Key */}
        <div className="rounded-[--radius-md] border border-border bg-bg-surface p-4">
          <h3 className="text-sm font-semibold text-text-primary mb-1">
            {t("setup.settings.metadataProvider")}
          </h3>
          <p className="text-xs text-text-muted mb-3">
            {t("setup.settings.metadataDescription")}
          </p>
          <Input
            label={t("setup.settings.tmdbApiKey")}
            type="text"
            value={tmdbApiKey}
            onChange={(e) => setTmdbApiKey(e.target.value)}
            placeholder={t("setup.settings.tmdbPlaceholder")}
            hint={t("setup.settings.tmdbHint")}
          />
        </div>

        {/* FFmpeg / Transcoding */}
        <div className="rounded-[--radius-md] border border-border bg-bg-surface p-4">
          <h3 className="text-sm font-semibold text-text-primary mb-1">
            {t("setup.settings.transcoding")}
          </h3>
          <p className="text-xs text-text-muted mb-3">
            {t("setup.settings.transcodingDescription")}
          </p>

          {capabilities.isLoading && (
            <div className="flex items-center gap-3 py-4">
              <Spinner size="sm" />
              <span className="text-sm text-text-secondary">
                Detecting system capabilities...
              </span>
            </div>
          )}

          {capabilities.isError && (
            <div className="flex items-center gap-2 rounded-[--radius-sm] bg-error/10 px-3 py-2">
              <svg
                className="h-4 w-4 shrink-0 text-error"
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path
                  fillRule="evenodd"
                  d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-5a.75.75 0 01.75.75v4.5a.75.75 0 01-1.5 0v-4.5A.75.75 0 0110 5zm0 10a1 1 0 100-2 1 1 0 000 2z"
                  clipRule="evenodd"
                />
              </svg>
              <span className="text-sm text-error">
                Unable to detect system capabilities.
              </span>
            </div>
          )}

          {capabilities.isSuccess && (
            <div className="flex flex-col gap-3">
              {/* FFmpeg status */}
              <div className="flex items-center gap-3">
                {ffmpegFound ? (
                  <>
                    <div className="flex h-8 w-8 items-center justify-center rounded-full bg-success/10">
                      <svg
                        className="h-4 w-4 text-success"
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
                    <div>
                      <p className="text-sm font-medium text-text-primary">
                        FFmpeg found
                      </p>
                      <p className="text-xs text-text-muted font-mono">
                        {ffmpegPath}
                      </p>
                    </div>
                    <Badge variant="success" className="ml-auto">
                      Detected
                    </Badge>
                  </>
                ) : (
                  <>
                    <div className="flex h-8 w-8 items-center justify-center rounded-full bg-warning/10">
                      <svg
                        className="h-4 w-4 text-warning"
                        viewBox="0 0 20 20"
                        fill="currentColor"
                      >
                        <path
                          fillRule="evenodd"
                          d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.168 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 6a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 6zm0 9a1 1 0 100-2 1 1 0 000 2z"
                          clipRule="evenodd"
                        />
                      </svg>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-text-primary">
                        FFmpeg not found
                      </p>
                      <p className="text-xs text-text-muted">
                        Transcoding will not be available. Install FFmpeg and
                        restart the server.
                      </p>
                    </div>
                    <Badge variant="warning" className="ml-auto">
                      Missing
                    </Badge>
                  </>
                )}
              </div>

              {/* Hardware Acceleration */}
              {ffmpegFound && hwAccels.length > 0 && (
                <div className="mt-2">
                  <p className="text-sm font-medium text-text-secondary mb-2">
                    Hardware Acceleration
                  </p>
                  <div className="flex flex-col gap-2">
                    <label className="flex items-center gap-3 rounded-[--radius-sm] px-3 py-2 hover:bg-bg-elevated transition-colors cursor-pointer">
                      <input
                        type="radio"
                        name="hwAccel"
                        value=""
                        checked={hwAccel === ""}
                        onChange={(e) => setHwAccel(e.target.value)}
                        className="text-accent focus:ring-accent"
                      />
                      <span className="text-sm text-text-primary">
                        None (software encoding)
                      </span>
                    </label>

                    {hwAccels.map((accel) => (
                      <label
                        key={accel}
                        className="flex items-center gap-3 rounded-[--radius-sm] px-3 py-2 hover:bg-bg-elevated transition-colors cursor-pointer"
                      >
                        <input
                          type="radio"
                          name="hwAccel"
                          value={accel}
                          checked={hwAccel === accel}
                          onChange={(e) => setHwAccel(e.target.value)}
                          className="text-accent focus:ring-accent"
                        />
                        <span className="text-sm text-text-primary uppercase">
                          {accel}
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {serverError && (
          <p className="rounded-[--radius-sm] bg-error/10 px-3 py-2 text-sm text-error">
            {serverError}
          </p>
        )}

        {/* Navigation buttons */}
        <div className="flex items-center justify-between pt-2">
          <Button type="button" variant="ghost" onClick={onBack}>
            Back
          </Button>

          <div className="flex items-center gap-3">
            <Button type="button" variant="ghost" onClick={handleSkip}>
              Skip
            </Button>
            <Button
              type="submit"
              size="lg"
              isLoading={setupSettings.isPending}
            >
              Save & Continue
            </Button>
          </div>
        </div>
      </form>
    </div>
  );
}
