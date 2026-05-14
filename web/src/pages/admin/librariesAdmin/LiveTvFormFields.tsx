// LiveTvFormFields — controlled subform with all the livetv-specific
// inputs of the admin "create library" flow: public (iptv-org) vs
// custom source picker, kind tabs (country / category / language /
// region), M3U / EPG URLs, language allowlist, TLS-insecure toggle
// and the preflight probe button.
//
// Shared by the full-page admin form (LibraryNewPage) and by the
// "Personal IPTV list" shortcut modal in UsersAdmin so both surfaces
// offer the same options without two parallel implementations to
// keep in sync.
//
// Controlled by props: the parent owns the state. This keeps reset
// semantics, validation timing and submit wiring under the caller's
// control and lets the modal close-and-clear without juggling refs.
// State shape + the pure resolver live in `./liveTvFormState.ts` so
// this file only exports React components (Fast Refresh invariant).

import type { Dispatch, SetStateAction } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/common";
import { usePublicCountries } from "@/api/hooks";
import { FilteredSelect } from "./FilteredSelect";
import { LanguageMultiSelect } from "./LanguageMultiSelect";
import { PreflightButton } from "./PreflightButton";
import { TLSInsecureToggle } from "./TLSInsecureToggle";
import {
  IPTV_ORG_CATEGORIES,
  IPTV_ORG_LANGUAGES,
  IPTV_ORG_REGIONS,
} from "./constants";
import type { LiveTvFormState } from "./liveTvFormState";

interface Props {
  value: LiveTvFormState;
  onChange: Dispatch<SetStateAction<LiveTvFormState>>;
  /** Hide the preflight button when the parent doesn't have the
   *  vertical real estate for it (e.g. compact modal). Defaults to
   *  showing it. */
  showPreflight?: boolean;
}

export function LiveTvFormFields({
  value,
  onChange,
  showPreflight = true,
}: Props) {
  const { t } = useTranslation();
  const publicCountries = usePublicCountries({
    enabled: value.liveSource === "public",
  });

  function patch(p: Partial<LiveTvFormState>) {
    onChange((prev) => ({ ...prev, ...p }));
  }

  return (
    <>
      <div
        role="tablist"
        aria-label={t("admin.libraries.livetvSource", {
          defaultValue: "Fuente",
        })}
        className="flex gap-1 rounded-[--radius-md] border border-border bg-bg-surface p-1"
      >
        <button
          type="button"
          role="tab"
          aria-selected={value.liveSource === "public"}
          onClick={() => patch({ liveSource: "public" })}
          className={[
            "flex-1 rounded-[--radius-sm] px-3 py-1.5 text-xs font-medium transition-colors",
            value.liveSource === "public"
              ? "bg-accent/15 text-accent"
              : "text-text-secondary hover:text-text-primary",
          ].join(" ")}
        >
          {t("admin.libraries.livetvPublic", {
            defaultValue: "Público (iptv-org)",
          })}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={value.liveSource === "custom"}
          onClick={() => patch({ liveSource: "custom" })}
          className={[
            "flex-1 rounded-[--radius-sm] px-3 py-1.5 text-xs font-medium transition-colors",
            value.liveSource === "custom"
              ? "bg-accent/15 text-accent"
              : "text-text-secondary hover:text-text-primary",
          ].join(" ")}
        >
          {t("admin.libraries.livetvCustom", {
            defaultValue: "Personalizada",
          })}
        </button>
      </div>

      {value.liveSource === "public" ? (
        <div className="flex flex-col gap-3">
          <div
            role="tablist"
            aria-label="Tipo de lista"
            className="grid grid-cols-4 gap-1 rounded-[--radius-md] border border-border bg-bg-surface p-1 text-xs"
          >
            {(
              [
                { k: "country", label: "País" },
                { k: "category", label: "Categoría" },
                { k: "language", label: "Idioma" },
                { k: "region", label: "Región" },
              ] as const
            ).map(({ k, label }) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={value.liveKind === k}
                onClick={() =>
                  patch({
                    liveKind: k,
                    liveFilter: "",
                    country: "",
                    livePick: "",
                  })
                }
                className={[
                  "rounded-[--radius-sm] px-2 py-1 font-medium transition-colors",
                  value.liveKind === k
                    ? "bg-accent/15 text-accent"
                    : "text-text-secondary hover:text-text-primary",
                ].join(" ")}
              >
                {label}
              </button>
            ))}
          </div>

          <Input
            label={t("admin.libraries.searchList", {
              defaultValue: "Filtrar",
            })}
            placeholder={t("admin.libraries.searchListPlaceholder", {
              defaultValue: "Escribe para filtrar…",
            })}
            value={value.liveFilter}
            onChange={(e) => patch({ liveFilter: e.target.value })}
          />

          {value.liveKind === "country" ? (
            <FilteredSelect
              id="livetv-country"
              label={t("admin.libraries.country", { defaultValue: "País" })}
              value={value.country}
              onChange={(v) => patch({ country: v })}
              filter={value.liveFilter}
              loading={publicCountries.isLoading}
              options={(publicCountries.data ?? []).map((c) => ({
                code: c.code,
                name: `${c.flag} ${c.name}`,
              }))}
            />
          ) : value.liveKind === "category" ? (
            <FilteredSelect
              id="livetv-category"
              label="Categoría"
              value={value.livePick}
              onChange={(v) => patch({ livePick: v })}
              filter={value.liveFilter}
              options={IPTV_ORG_CATEGORIES}
            />
          ) : value.liveKind === "language" ? (
            <FilteredSelect
              id="livetv-language"
              label="Idioma"
              value={value.livePick}
              onChange={(v) => patch({ livePick: v })}
              filter={value.liveFilter}
              options={IPTV_ORG_LANGUAGES}
            />
          ) : (
            <FilteredSelect
              id="livetv-region"
              label="Región"
              value={value.livePick}
              onChange={(v) => patch({ livePick: v })}
              filter={value.liveFilter}
              options={IPTV_ORG_REGIONS}
            />
          )}
        </div>
      ) : (
        <Input
          label={t("admin.libraries.m3uUrl", { defaultValue: "URL M3U" })}
          placeholder="https://ejemplo.com/playlist.m3u"
          value={value.m3uURL}
          onChange={(e) => patch({ m3uURL: e.target.value })}
          required
        />
      )}

      <div className="flex flex-col gap-1">
        <Input
          label={t("admin.libraries.epgUrl", {
            defaultValue: "URL EPG (opcional)",
          })}
          placeholder="https://ejemplo.com/epg.xml"
          value={value.epgURL}
          onChange={(e) => patch({ epgURL: e.target.value })}
        />
        <p className="text-[11px] leading-snug text-text-muted">
          {t("admin.libraries.epgURLHint", {
            defaultValue:
              "Si el M3U trae url-tvg en su cabecera, se auto-detecta.",
          })}
        </p>
      </div>

      <LanguageMultiSelect
        value={value.languageFilter}
        onChange={(v) => patch({ languageFilter: v })}
      />
      <TLSInsecureToggle
        value={value.tlsInsecure}
        onChange={(v) => patch({ tlsInsecure: v })}
      />

      {showPreflight && value.liveSource === "custom" && (
        <PreflightButton
          m3uURL={value.m3uURL}
          tlsInsecure={value.tlsInsecure}
        />
      )}
    </>
  );
}
