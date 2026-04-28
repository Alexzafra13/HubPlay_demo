// LibraryFormModal — the "Add library" modal.
//
// Owns its own form state (name / type / path / livetv-source / iptv-org
// kind+pick / custom M3U+EPG URLs). The parent only knows whether it's
// open and gets a callback when a new library has been created. This
// matches the Edit modal's contract: state is local, the parent stays
// thin.

import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Button, Input, Modal } from "@/components/common";
import { FolderBrowser } from "@/components/setup/FolderBrowser";
import { useCreateLibrary, useRefreshM3U, usePublicCountries } from "@/api/hooks";
import type { ContentType, Library } from "@/api/types";
import { FilteredSelect } from "./FilteredSelect";
import {
  CONTENT_TYPES,
  IPTV_ORG_CATEGORIES,
  IPTV_ORG_LANGUAGES,
  IPTV_ORG_PATH_BY_KIND,
  IPTV_ORG_REGIONS,
  type LiveKind,
  type LiveSource,
} from "./constants";

interface LibraryFormModalProps {
  isOpen: boolean;
  onClose: () => void;
  onCreated: (lib: Library) => void;
}

export function LibraryFormModal({ isOpen, onClose, onCreated }: LibraryFormModalProps) {
  const { t } = useTranslation();
  const createLibrary = useCreateLibrary();
  const refreshM3U = useRefreshM3U();

  const [name, setName] = useState("");
  const [type, setType] = useState<ContentType>("movies");
  const [path, setPath] = useState("");
  const [showBrowse, setShowBrowse] = useState(false);
  // livetv-specific: "public" = iptv-org country picker, "custom" = paste URLs.
  const [liveSource, setLiveSource] = useState<LiveSource>("public");
  // iptv-org supports four URL families: countries/categories/languages/regions.
  // Keeping one state per kind lets the user switch tabs without losing typed
  // input, and the single search filter `liveFilter` scopes to whatever is
  // currently active.
  const [liveKind, setLiveKind] = useState<LiveKind>("country");
  const [liveFilter, setLiveFilter] = useState("");
  const [country, setCountry] = useState("");
  const [livePick, setLivePick] = useState("");
  const [m3uURL, setM3UURL] = useState("");
  const [epgURL, setEPGURL] = useState("");

  // Only fires the network request while the Add modal is open AND the user
  // has picked livetv — avoids loading the 200-country list for every admin
  // page view.
  const publicCountries = usePublicCountries({
    enabled: isOpen && type === "livetv" && liveSource === "public",
  });

  // Reset whenever the modal closes so the next open starts clean. Tied
  // to isOpen rather than a manual reset call so callers can't forget.
  useEffect(() => {
    if (isOpen) return;
    setName("");
    setType("movies");
    setPath("");
    setLiveSource("public");
    setLiveKind("country");
    setLiveFilter("");
    setCountry("");
    setLivePick("");
    setM3UURL("");
    setEPGURL("");
  }, [isOpen]);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;

    if (type === "livetv") {
      // Resolve the M3U URL depending on the chosen source.
      let resolvedM3U = "";
      if (liveSource === "public") {
        const pick = liveKind === "country" ? country : livePick;
        if (!pick) return;
        resolvedM3U = `https://iptv-org.github.io/iptv/${IPTV_ORG_PATH_BY_KIND[liveKind]}/${pick}.m3u`;
      } else {
        if (!m3uURL.trim()) return;
        resolvedM3U = m3uURL.trim();
      }
      createLibrary.mutate(
        {
          name: name.trim(),
          content_type: "livetv",
          paths: [],
          m3u_url: resolvedM3U,
          epg_url: epgURL.trim() || undefined,
        },
        {
          onSuccess: (lib) => {
            onCreated(lib);
            // Auto-trigger the first M3U refresh so the library isn't empty
            // the moment the admin closes the modal.
            refreshM3U.mutate(lib.id);
          },
        },
      );
      return;
    }

    // Non-livetv: path-based library.
    if (!path.trim()) return;
    createLibrary.mutate(
      { name: name.trim(), content_type: type, paths: [path.trim()] },
      { onSuccess: (lib) => onCreated(lib) },
    );
  }

  return (
    <>
      <Modal isOpen={isOpen} onClose={onClose} title={t("admin.libraries.addLibrary")}>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Input
            label={t("admin.libraries.name")}
            placeholder={t("admin.libraries.namePlaceholder")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="content-type"
              className="text-sm font-medium text-text-secondary"
            >
              {t("admin.libraries.contentType")}
            </label>
            <select
              id="content-type"
              value={type}
              onChange={(e) => setType(e.target.value as ContentType)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              {CONTENT_TYPES.map((ct) => (
                <option key={ct.value} value={ct.value}>
                  {t(ct.key)}
                </option>
              ))}
            </select>
          </div>

          {type === "livetv" ? (
            <>
              {/* Source tabs — Público (iptv-org) vs Personalizada */}
              <div
                role="tablist"
                aria-label={t("admin.libraries.livetvSource", { defaultValue: "Fuente" })}
                className="flex gap-1 rounded-[--radius-md] border border-border bg-bg-surface p-1"
              >
                <button
                  type="button"
                  role="tab"
                  aria-selected={liveSource === "public"}
                  onClick={() => setLiveSource("public")}
                  className={[
                    "flex-1 rounded-[--radius-sm] px-3 py-1.5 text-xs font-medium transition-colors",
                    liveSource === "public"
                      ? "bg-accent/15 text-accent"
                      : "text-text-secondary hover:text-text-primary",
                  ].join(" ")}
                >
                  {t("admin.libraries.livetvPublic", { defaultValue: "Público (iptv-org)" })}
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={liveSource === "custom"}
                  onClick={() => setLiveSource("custom")}
                  className={[
                    "flex-1 rounded-[--radius-sm] px-3 py-1.5 text-xs font-medium transition-colors",
                    liveSource === "custom"
                      ? "bg-accent/15 text-accent"
                      : "text-text-secondary hover:text-text-primary",
                  ].join(" ")}
                >
                  {t("admin.libraries.livetvCustom", { defaultValue: "Personalizada" })}
                </button>
              </div>

              {liveSource === "public" ? (
                <div className="flex flex-col gap-3">
                  {/* Kind selector — iptv-org has 4 URL families */}
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
                        aria-selected={liveKind === k}
                        onClick={() => {
                          setLiveKind(k);
                          setLiveFilter("");
                          setCountry("");
                          setLivePick("");
                        }}
                        className={[
                          "rounded-[--radius-sm] px-2 py-1 font-medium transition-colors",
                          liveKind === k
                            ? "bg-accent/15 text-accent"
                            : "text-text-secondary hover:text-text-primary",
                        ].join(" ")}
                      >
                        {label}
                      </button>
                    ))}
                  </div>

                  {/* Live filter input — works across all four kinds. */}
                  <Input
                    label={t("admin.libraries.searchList", { defaultValue: "Filtrar" })}
                    placeholder={t("admin.libraries.searchListPlaceholder", {
                      defaultValue: "Escribe para filtrar…",
                    })}
                    value={liveFilter}
                    onChange={(e) => setLiveFilter(e.target.value)}
                  />

                  {/* The actual picker. Separate branches keep each select's
                      options and selected value independent — switching tabs
                      doesn't wipe the filter but does reset the pick. */}
                  {liveKind === "country" ? (
                    <FilteredSelect
                      id="livetv-country"
                      label={t("admin.libraries.country", { defaultValue: "País" })}
                      value={country}
                      onChange={setCountry}
                      filter={liveFilter}
                      loading={publicCountries.isLoading}
                      options={(publicCountries.data ?? []).map((c) => ({
                        code: c.code,
                        name: `${c.flag} ${c.name}`,
                      }))}
                    />
                  ) : liveKind === "category" ? (
                    <FilteredSelect
                      id="livetv-category"
                      label="Categoría"
                      value={livePick}
                      onChange={setLivePick}
                      filter={liveFilter}
                      options={IPTV_ORG_CATEGORIES}
                    />
                  ) : liveKind === "language" ? (
                    <FilteredSelect
                      id="livetv-language"
                      label="Idioma"
                      value={livePick}
                      onChange={setLivePick}
                      filter={liveFilter}
                      options={IPTV_ORG_LANGUAGES}
                    />
                  ) : (
                    <FilteredSelect
                      id="livetv-region"
                      label="Región"
                      value={livePick}
                      onChange={setLivePick}
                      filter={liveFilter}
                      options={IPTV_ORG_REGIONS}
                    />
                  )}

                  <p className="text-[11px] text-text-muted">
                    {t("admin.libraries.publicIPTVHint", {
                      defaultValue:
                        "Playlists públicas del proyecto iptv-org. Puedes añadir varias (p. ej. España + Francia + Deportes).",
                    })}
                  </p>
                </div>
              ) : (
                <Input
                  label={t("admin.libraries.m3uUrl", { defaultValue: "URL M3U" })}
                  placeholder="https://ejemplo.com/playlist.m3u"
                  value={m3uURL}
                  onChange={(e) => setM3UURL(e.target.value)}
                  required
                />
              )}

              <Input
                label={t("admin.libraries.epgUrl", { defaultValue: "URL EPG (opcional)" })}
                placeholder="https://ejemplo.com/epg.xml"
                value={epgURL}
                onChange={(e) => setEPGURL(e.target.value)}
              />
              <p className="-mt-2 text-[11px] text-text-muted">
                {t("admin.libraries.epgURLHint", {
                  defaultValue: "Si el M3U trae url-tvg en su cabecera, se auto-detecta.",
                })}
              </p>
            </>
          ) : (
            <div className="flex gap-2 items-end">
              <div className="flex-1">
                <Input
                  label={t("admin.libraries.path")}
                  placeholder={t("admin.libraries.pathPlaceholder")}
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  required
                />
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setShowBrowse(true)}
              >
                {t("common.browse")}
              </Button>
            </div>
          )}

          {createLibrary.error && (
            <p className="text-xs text-error">{createLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button variant="secondary" type="button" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" isLoading={createLibrary.isPending}>
              {t("common.create")}
            </Button>
          </div>
        </form>
      </Modal>

      <FolderBrowser
        isOpen={showBrowse}
        onClose={() => setShowBrowse(false)}
        onSelect={(picked) => {
          setPath(picked);
          // Auto-fill the name from the trailing path segment when
          // the user hasn't typed one yet — saves a round-trip to the
          // name field on the common case.
          if (!name.trim()) {
            const segments = picked.split("/").filter(Boolean);
            setName(segments[segments.length - 1] ?? "");
          }
        }}
        useAdmin
      />
    </>
  );
}
