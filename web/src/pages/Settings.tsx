import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/store/auth";
import { useLibraries, useScanLibrary, useProviders, useUpdateProvider } from "@/api/hooks";
import type { Library } from "@/api/types";
import { Badge, Button, Spinner } from "@/components/common";

function getPathAccessible(lib: Library, path: string): boolean | undefined {
  const status = lib.path_status?.find((ps) => ps.path === path);
  return status?.accessible;
}

function ProviderSettings() {
  const { t } = useTranslation();
  const { data: providers, isLoading } = useProviders();
  const updateProvider = useUpdateProvider();
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState<Record<string, boolean>>({});

  const providerMeta: Record<string, { label: string; description: string; url: string }> = {
    tmdb: {
      label: "TMDB (The Movie Database)",
      description: "Posters, backdrops, synopses, ratings, cast & crew",
      url: "https://www.themoviedb.org/settings/api",
    },
    opensubtitles: {
      label: "OpenSubtitles",
      description: "Automatic subtitle downloads",
      url: "https://www.opensubtitles.com/consumers",
    },
  };

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {providers?.map((p) => {
        const meta = providerMeta[p.name] ?? {
          label: p.name,
          description: p.type,
          url: "",
        };
        const isSaved = saved[p.name];

        return (
          <div
            key={p.name}
            className="rounded-[--radius-lg] border border-border bg-bg-card p-4"
          >
            <div className="flex items-center justify-between mb-2">
              <div className="flex items-center gap-2">
                <span className="font-medium text-text-primary">
                  {meta.label}
                </span>
                <Badge
                  variant={
                    p.has_api_key && p.status === "active"
                      ? "success"
                      : "default"
                  }
                >
                  {p.has_api_key ? t('settings.configured') : t('settings.notConfigured')}
                </Badge>
              </div>
            </div>

            <p className="text-xs text-text-muted mb-3">{meta.description}</p>

            <div className="flex items-end gap-2">
              <div className="flex-1">
                <label
                  htmlFor={`api-key-${p.name}`}
                  className="block text-xs font-medium text-text-secondary mb-1"
                >
                  {t('settings.apiKey')}
                </label>
                <input
                  id={`api-key-${p.name}`}
                  type="password"
                  placeholder={p.has_api_key ? "••••••••••••" : "Enter API key"}
                  value={apiKeys[p.name] ?? ""}
                  onChange={(e) => {
                    setApiKeys((prev) => ({
                      ...prev,
                      [p.name]: e.target.value,
                    }));
                    setSaved((prev) => ({ ...prev, [p.name]: false }));
                  }}
                  className="w-full rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                />
              </div>
              <Button
                variant="secondary"
                size="sm"
                disabled={!apiKeys[p.name]}
                isLoading={
                  updateProvider.isPending &&
                  (updateProvider.variables as { name: string })?.name ===
                    p.name
                }
                onClick={() => {
                  updateProvider.mutate(
                    {
                      name: p.name,
                      data: {
                        api_key: apiKeys[p.name],
                        status: "active",
                      },
                    },
                    {
                      onSuccess: () => {
                        setApiKeys((prev) => ({ ...prev, [p.name]: "" }));
                        setSaved((prev) => ({ ...prev, [p.name]: true }));
                        setTimeout(
                          () =>
                            setSaved((prev) => ({
                              ...prev,
                              [p.name]: false,
                            })),
                          3000,
                        );
                      },
                    },
                  );
                }}
              >
                {isSaved ? "OK" : t('common.save')}
              </Button>
            </div>

            {meta.url && (
              <p className="mt-2 text-xs text-text-muted">
                Get your API key at{" "}
                <a
                  href={meta.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-accent hover:underline"
                >
                  {new URL(meta.url).hostname}
                </a>
              </p>
            )}
          </div>
        );
      })}

      {(!providers || providers.length === 0) && (
        <div className="rounded-[--radius-lg] border border-border bg-bg-card p-6 text-center">
          <p className="text-sm text-text-muted">No providers available.</p>
        </div>
      )}

      <div className="rounded-md bg-accent/10 border border-accent/20 px-3 py-2">
        <p className="text-xs text-text-secondary">
          {t('settings.metadataHint')}
        </p>
      </div>
    </div>
  );
}

export default function Settings() {
  const { t } = useTranslation();
  const { user } = useAuthStore();
  const isAdmin = user?.role === "admin";
  const { data: libraries, isLoading: libsLoading } = useLibraries();
  const scanLibrary = useScanLibrary();

  return (
    <div className="flex flex-col gap-8 px-6 py-8 sm:px-10 max-w-4xl">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        {t('settings.title')}
      </h1>

      {/* Account Info */}
      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">{t('settings.account')}</h2>
        <div className="rounded-[--radius-lg] border border-border bg-bg-card divide-y divide-border">
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-text-muted">{t('settings.username')}</span>
            <span className="text-sm font-medium text-text-primary">
              {user?.username}
            </span>
          </div>
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-text-muted">{t('settings.displayName')}</span>
            <span className="text-sm font-medium text-text-primary">
              {user?.display_name || "\u2014"}
            </span>
          </div>
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-text-muted">{t('settings.role')}</span>
            <Badge variant={user?.role === "admin" ? "warning" : "default"}>
              {user?.role}
            </Badge>
          </div>
        </div>
      </section>

      {/* Metadata Providers (admin only) */}
      {isAdmin && (
        <section className="flex flex-col gap-4">
          <h2 className="text-lg font-semibold text-text-primary">
            {t('settings.metadataProviders')}
          </h2>
          <ProviderSettings />
        </section>
      )}

      {/* Libraries Overview */}
      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          {t('settings.mediaLibraries')}
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
                          scanLibrary.variables?.id === lib.id
                        }
                        onClick={() => scanLibrary.mutate({ id: lib.id })}
                      >
                        {t('settings.scanNow')}
                      </Button>
                    )}
                  </div>

                  <div className="flex flex-col gap-1.5">
                    {(lib.paths ?? []).map((p) => {
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
                              {t('settings.pathNotFound')}
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
                      {t('settings.items', { count: lib.item_count ?? 0 })}
                    </span>
                    <span className="text-xs text-text-muted">
                      {t('settings.scanMode', { mode: lib.scan_mode ?? "manual" })}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="rounded-[--radius-lg] border border-border bg-bg-card p-6 text-center">
            <p className="text-sm text-text-muted">
              {t('settings.noLibraries')}
              {isAdmin && ` ${t('settings.goToAdmin')}`}
            </p>
          </div>
        )}
      </section>
    </div>
  );
}
