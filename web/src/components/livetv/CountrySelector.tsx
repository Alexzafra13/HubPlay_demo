import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import {
  useImportPublicIPTV,
  usePublicCountries,
  queryKeys,
} from "@/api/hooks";
import type { PublicCountry } from "@/api/types";
import { Spinner } from "@/components/common";
import { detectCountryCode } from "./detectCountry";

interface CountrySelectorProps {
  hasLibrary: boolean;
}

export function CountrySelector({ hasLibrary }: CountrySelectorProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: countries, isLoading } = usePublicCountries();
  const importMutation = useImportPublicIPTV();
  const [selectedCountry, setSelectedCountry] = useState<PublicCountry | null>(null);
  const [countrySearch, setCountrySearch] = useState("");
  const [autoDetected, setAutoDetected] = useState(false);

  useEffect(() => {
    if (!countries || countries.length === 0 || autoDetected) return;
    const code = detectCountryCode();
    const match = countries.find((c) => c.code === code);
    if (match) setSelectedCountry(match);
    setAutoDetected(true);
  }, [countries, autoDetected]);

  const filtered = useMemo(() => {
    if (!countries) return [];
    if (!countrySearch) return countries;
    return countries.filter(
      (c) =>
        c.name.toLowerCase().includes(countrySearch.toLowerCase()) ||
        c.code.toLowerCase().includes(countrySearch.toLowerCase()),
    );
  }, [countries, countrySearch]);

  const handleImport = () => {
    if (!selectedCountry) return;
    importMutation.mutate(
      { country: selectedCountry.code },
      {
        onSuccess: () => {
          // Refresh libraries + channels in place instead of reloading the
          // whole page (which dropped auth state and back history).
          queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
          queryClient.invalidateQueries({
            predicate: (q) =>
              Array.isArray(q.queryKey) && q.queryKey[0] === "channels",
          });
        },
      },
    );
  };

  return (
    <div className="flex min-h-[60vh] items-center justify-center px-4">
      <div className="w-full max-w-lg">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-5 w-20 h-20 rounded-2xl bg-accent/10 flex items-center justify-center">
            <svg
              width="40"
              height="40"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              className="text-accent"
            >
              <rect x="2" y="4" width="20" height="14" rx="2" />
              <path d="M7 22h10M12 18v4" />
            </svg>
          </div>
          <h2 className="text-2xl font-bold text-text-primary">
            {hasLibrary ? t("liveTV.noChannelsLoaded") : t("liveTV.setupLiveTV")}
          </h2>
          <p className="mt-2 text-sm text-text-muted max-w-sm mx-auto">
            {t("liveTV.importDescription")}
            {selectedCountry && !countrySearch && (
              <span className="block mt-1 text-accent">
                {t("liveTV.detectedCountry", {
                  flag: selectedCountry.flag,
                  country: selectedCountry.name,
                })}
              </span>
            )}
          </p>
        </div>

        <div className="rounded-2xl border border-white/10 bg-white/[0.03] backdrop-blur-sm p-5">
          <label className="sr-only" htmlFor="country-search">
            {t("liveTV.searchCountries")}
          </label>
          <input
            id="country-search"
            type="text"
            placeholder={t("liveTV.searchCountries")}
            value={countrySearch}
            onChange={(e) => setCountrySearch(e.target.value)}
            className="mb-4 w-full rounded-xl bg-white/5 border border-white/10 px-4 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30 transition-all"
          />

          {isLoading ? (
            <div className="flex justify-center py-8">
              <Spinner size="md" />
            </div>
          ) : (
            <div className="grid max-h-60 grid-cols-2 gap-2 overflow-y-auto sm:grid-cols-3 pr-1">
              {filtered.map((country) => (
                <button
                  key={country.code}
                  type="button"
                  onClick={() => setSelectedCountry(country)}
                  aria-pressed={selectedCountry?.code === country.code}
                  className={[
                    "rounded-xl border px-3 py-2.5 text-left text-sm transition-all",
                    selectedCountry?.code === country.code
                      ? "border-accent bg-accent/10 text-text-primary ring-1 ring-accent/30"
                      : "border-white/10 bg-white/[0.02] text-text-secondary hover:bg-white/5 hover:text-text-primary",
                  ].join(" ")}
                >
                  <span className="mr-1.5">{country.flag}</span>
                  {country.name}
                </button>
              ))}
            </div>
          )}

          {selectedCountry && (
            <div className="mt-5 flex items-center justify-between gap-3">
              <span className="text-sm text-text-secondary truncate">
                {selectedCountry.flag} <strong>{selectedCountry.name}</strong>
              </span>
              <button
                type="button"
                onClick={handleImport}
                disabled={importMutation.isPending}
                className="shrink-0 rounded-xl bg-accent px-5 py-2.5 text-sm font-medium text-white transition-all hover:bg-accent/90 hover:shadow-lg hover:shadow-accent/20 disabled:opacity-50"
              >
                {importMutation.isPending ? (
                  <span className="flex items-center gap-2">
                    <Spinner size="sm" /> {t("liveTV.importing")}
                  </span>
                ) : (
                  t("liveTV.importChannels")
                )}
              </button>
            </div>
          )}

          {importMutation.isError && (
            <p className="mt-3 text-sm text-error">{t("liveTV.importFailed")}</p>
          )}

          {importMutation.isSuccess && (
            <p className="mt-3 text-sm text-accent">
              {t("liveTV.importSuccess")}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
