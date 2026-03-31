import { useState } from "react";
import { useProviders, useUpdateProvider } from "@/api/hooks";
import { Badge, Spinner } from "@/components/common";
import { Button } from "@/components/common/Button";
import { useTranslation } from 'react-i18next';

const LANGUAGES = [
  { code: "es-ES", label: "Español" },
  { code: "en-US", label: "English" },
  { code: "fr-FR", label: "Français" },
  { code: "de-DE", label: "Deutsch" },
  { code: "it-IT", label: "Italiano" },
  { code: "pt-BR", label: "Português (Brasil)" },
  { code: "pt-PT", label: "Português" },
  { code: "ja-JP", label: "日本語" },
  { code: "ko-KR", label: "한국어" },
  { code: "zh-CN", label: "中文 (简体)" },
  { code: "ru-RU", label: "Русский" },
  { code: "nl-NL", label: "Nederlands" },
  { code: "pl-PL", label: "Polski" },
  { code: "sv-SE", label: "Svenska" },
  { code: "da-DK", label: "Dansk" },
  { code: "fi-FI", label: "Suomi" },
  { code: "nb-NO", label: "Norsk" },
  { code: "tr-TR", label: "Türkçe" },
  { code: "ar-SA", label: "العربية" },
  { code: "hi-IN", label: "हिन्दी" },
];

const PROVIDER_INFO: Record<string, { label: string; descriptionKey: string; keyPlaceholder: string; docsUrl: string; hasLanguage: boolean }> = {
  tmdb: {
    label: "TMDb",
    descriptionKey: "admin.providers.tmdbDescription",
    keyPlaceholder: "Enter your TMDb API key (v3 auth)",
    docsUrl: "https://www.themoviedb.org/settings/api",
    hasLanguage: true,
  },
  fanart: {
    label: "Fanart.tv",
    descriptionKey: "admin.providers.fanartDescription",
    keyPlaceholder: "Enter your Fanart.tv API key",
    docsUrl: "https://fanart.tv/get-an-api-key/",
    hasLanguage: false,
  },
  opensubtitles: {
    label: "OpenSubtitles",
    descriptionKey: "admin.providers.opensubsDescription",
    keyPlaceholder: "Enter your OpenSubtitles API key",
    docsUrl: "https://www.opensubtitles.com/en/consumers",
    hasLanguage: false,
  },
};

interface ProviderData {
  name: string;
  type: string;
  status: string;
  priority: number;
  has_api_key: boolean;
  config?: Record<string, string>;
}

function ProviderCard({ provider }: { provider: ProviderData }) {
  const { t } = useTranslation();
  const info = PROVIDER_INFO[provider.name] ?? {
    label: provider.name,
    descriptionKey: null,
    keyPlaceholder: "Enter API key",
    docsUrl: "",
    hasLanguage: false,
  };

  const [apiKey, setApiKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [saved, setSaved] = useState(false);

  const currentLang = provider.config?.language ?? "en-US";

  const updateProvider = useUpdateProvider();

  const handleSaveKey = () => {
    if (!apiKey.trim()) return;
    updateProvider.mutate(
      { name: provider.name, data: { api_key: apiKey.trim() } },
      {
        onSuccess: () => {
          setSaved(true);
          setApiKey("");
          setTimeout(() => setSaved(false), 3000);
        },
      },
    );
  };

  const handleToggleStatus = () => {
    const newStatus = provider.status === "active" ? "disabled" : "active";
    updateProvider.mutate({ name: provider.name, data: { status: newStatus } });
  };

  const handleLanguageChange = (lang: string) => {
    updateProvider.mutate({
      name: provider.name,
      data: { config: { language: lang } },
    });
  };

  const isActive = provider.status === "active";

  const description = info.descriptionKey
    ? t(info.descriptionKey)
    : `${provider.type} provider`;

  return (
    <div className="flex flex-col gap-4 rounded-[--radius-lg] border border-border bg-bg-card p-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-3">
            <h3 className="text-base font-semibold text-text-primary">
              {info.label}
            </h3>
            <Badge variant={isActive ? "success" : "default"}>
              {isActive ? t('admin.providers.active') : t('admin.providers.disabled')}
            </Badge>
            {provider.has_api_key && (
              <Badge>{t('admin.providers.apiKeySet')}</Badge>
            )}
          </div>
          <p className="text-sm text-text-secondary">{description}</p>
        </div>

        <button
          type="button"
          onClick={handleToggleStatus}
          disabled={updateProvider.isPending}
          className={[
            "relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-accent focus:ring-offset-2 focus:ring-offset-bg-base",
            isActive ? "bg-accent" : "bg-bg-elevated",
          ].join(" ")}
          role="switch"
          aria-checked={isActive}
        >
          <span
            className={[
              "pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out",
              isActive ? "translate-x-5" : "translate-x-0",
            ].join(" ")}
          />
        </button>
      </div>

      {/* API Key input */}
      <div className="flex flex-col gap-2">
        <label className="text-xs font-medium uppercase tracking-wider text-text-muted">
          {t('admin.providers.apiKey')}
        </label>
        <div className="flex gap-2">
          <div className="relative flex-1">
            <input
              type={showKey ? "text" : "password"}
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={
                provider.has_api_key
                  ? "••••••••••••••••  (key is set — enter new to replace)"
                  : info.keyPlaceholder
              }
              className="w-full rounded-[--radius-md] border border-border bg-bg-base px-3 py-2 pr-10 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSaveKey();
              }}
            />
            <button
              type="button"
              onClick={() => setShowKey(!showKey)}
              className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-text-muted hover:text-text-primary"
              aria-label={showKey ? t('admin.providers.hideKey') : t('admin.providers.showKey')}
            >
              {showKey ? (
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
                </svg>
              ) : (
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z" />
                  <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                </svg>
              )}
            </button>
          </div>
          <Button
            size="sm"
            onClick={handleSaveKey}
            disabled={!apiKey.trim() || updateProvider.isPending}
          >
            {updateProvider.isPending ? t('common.saving') : t('common.save')}
          </Button>
        </div>
        {info.docsUrl && (
          <a
            href={info.docsUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-accent hover:underline"
          >
            {t('admin.providers.getApiKey')}
          </a>
        )}
        {saved && (
          <span className="text-xs text-success">{t('admin.providers.apiKeySaved')}</span>
        )}
        {updateProvider.isError && (
          <span className="text-xs text-error">
            {t('admin.providers.saveFailed', { error: updateProvider.error?.message })}
          </span>
        )}
      </div>

      {/* Language selector (for providers that support it) */}
      {info.hasLanguage && (
        <div className="flex flex-col gap-2">
          <label className="text-xs font-medium uppercase tracking-wider text-text-muted">
            {t('admin.providers.preferredLanguage')}
          </label>
          <p className="text-xs text-text-muted">
            {t('admin.providers.languageHint')}
          </p>
          <select
            value={currentLang}
            onChange={(e) => handleLanguageChange(e.target.value)}
            className="w-full max-w-xs rounded-[--radius-md] border border-border bg-bg-base px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
          >
            {LANGUAGES.map((lang) => (
              <option key={lang.code} value={lang.code}>
                {lang.label}
              </option>
            ))}
          </select>
        </div>
      )}
    </div>
  );
}

export default function ProvidersAdmin() {
  const { t } = useTranslation();
  const { data: providers, isLoading, error } = useProviders();

  if (isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 py-16">
        <Badge variant="error">{t('admin.providers.error')}</Badge>
        <p className="text-sm text-text-muted">
          {error.message}
        </p>
      </div>
    );
  }

  if (!providers || providers.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          {t('admin.providers.title')}
        </h2>
        <p className="text-sm text-text-secondary">
          {t('admin.providers.noProviders')}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h2 className="text-lg font-semibold text-text-primary">
          {t('admin.providers.title')}
        </h2>
        <p className="mt-1 text-sm text-text-secondary">
          {t('admin.providers.description')}
        </p>
      </div>

      <div className="flex flex-col gap-4">
        {providers.map((provider) => (
          <ProviderCard key={provider.name} provider={provider} />
        ))}
      </div>
    </div>
  );
}
