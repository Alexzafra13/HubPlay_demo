import { useState, useEffect } from "react";
import type { FormEvent } from "react";
import type { ContentType, Library } from "@/api/types";
import {
  useLibraries,
  useCreateLibrary,
  useUpdateLibrary,
  useScanLibrary,
  useDeleteLibrary,
  useRefreshLibraryImages,
  useRefreshM3U,
  useRefreshEPG,
  usePublicCountries,
} from "@/api/hooks";
import { Button, Badge, Modal, Input, Spinner, EmptyState } from "@/components/common";
import { FolderBrowser } from "@/components/setup/FolderBrowser";
import { useTranslation } from 'react-i18next';

// ─── iptv-org catalogues ───────────────────────────────────────────────────
//
// Curated slugs + labels for the four URL families iptv-org publishes.
// Countries come from the live API (usePublicCountries) because it's a
// long tail that rotates; the other three are stable enough to hardcode.
//
// URL patterns:
//   /iptv/countries/{code}.m3u     (code = ISO 3166-1 alpha-2, lowercase)
//   /iptv/categories/{slug}.m3u
//   /iptv/languages/{slug}.m3u     (slug = ISO 639-3)
//   /iptv/regions/{slug}.m3u

const IPTV_ORG_CATEGORIES: { code: string; name: string }[] = [
  { code: "general", name: "General" },
  { code: "news", name: "Informativos" },
  { code: "sports", name: "Deportes" },
  { code: "movies", name: "Películas" },
  { code: "series", name: "Series" },
  { code: "music", name: "Música" },
  { code: "kids", name: "Infantiles" },
  { code: "documentary", name: "Documentales" },
  { code: "entertainment", name: "Entretenimiento" },
  { code: "comedy", name: "Comedia" },
  { code: "business", name: "Negocios" },
  { code: "education", name: "Educación" },
  { code: "lifestyle", name: "Estilo de vida" },
  { code: "travel", name: "Viajes" },
  { code: "weather", name: "Tiempo" },
  { code: "science", name: "Ciencia" },
  { code: "religious", name: "Religioso" },
  { code: "shop", name: "Shopping" },
  { code: "cooking", name: "Cocina" },
  { code: "auto", name: "Motor" },
  { code: "animation", name: "Animación" },
  { code: "classic", name: "Clásicos" },
  { code: "family", name: "Familiar" },
  { code: "legislative", name: "Legislativo" },
  { code: "outdoor", name: "Exterior" },
  { code: "relax", name: "Relax" },
  { code: "xxx", name: "Adultos" },
];

const IPTV_ORG_LANGUAGES: { code: string; name: string }[] = [
  { code: "spa", name: "Español" },
  { code: "cat", name: "Catalán" },
  { code: "glg", name: "Gallego" },
  { code: "eus", name: "Euskera" },
  { code: "eng", name: "English" },
  { code: "por", name: "Portugués" },
  { code: "fra", name: "Français" },
  { code: "deu", name: "Deutsch" },
  { code: "ita", name: "Italiano" },
  { code: "nld", name: "Nederlands" },
  { code: "rus", name: "Русский" },
  { code: "ara", name: "العربية" },
  { code: "tur", name: "Türkçe" },
  { code: "pol", name: "Polski" },
  { code: "ell", name: "Ελληνικά" },
  { code: "ron", name: "Română" },
  { code: "ces", name: "Čeština" },
  { code: "hun", name: "Magyar" },
  { code: "swe", name: "Svenska" },
  { code: "nor", name: "Norsk" },
  { code: "dan", name: "Dansk" },
  { code: "fin", name: "Suomi" },
  { code: "ukr", name: "Українська" },
  { code: "heb", name: "עברית" },
  { code: "hin", name: "हिन्दी" },
  { code: "cmn", name: "中文 (Mandarin)" },
  { code: "jpn", name: "日本語" },
  { code: "kor", name: "한국어" },
  { code: "tha", name: "ภาษาไทย" },
  { code: "vie", name: "Tiếng Việt" },
];

const IPTV_ORG_REGIONS: { code: string; name: string }[] = [
  { code: "eur", name: "Europa" },
  { code: "amer", name: "América" },
  { code: "nam", name: "Norteamérica" },
  { code: "latam", name: "Latinoamérica" },
  { code: "afr", name: "África" },
  { code: "asia", name: "Asia" },
  { code: "seasia", name: "Sudeste Asiático" },
  { code: "oce", name: "Oceanía" },
  { code: "mena", name: "Oriente Medio y Norte de África" },
  { code: "carib", name: "Caribe" },
  { code: "nord", name: "Países Nórdicos" },
];

const CONTENT_TYPES: { value: ContentType; key: string }[] = [
  { value: "movies", key: "contentTypes.movies" },
  { value: "tvshows", key: "contentTypes.tvShows" },
  { value: "livetv", key: "contentTypes.liveTV" },
];

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

function contentTypeBadge(type: string) {
  switch (type) {
    case "movies":
      return "default" as const;
    case "tvshows":
      return "default" as const;
    case "livetv":
      return "live" as const;
    default:
      return "default" as const;
  }
}

// FilteredSelect — a native <select> whose option list is narrowed by a text
// filter provided from outside. Chose a native select over a custom combobox
// because it's keyboard-accessible, mobile-friendly, and matches the rest of
// the admin's visual language. The filter input lives in the parent so every
// kind (country/category/language/region) can share one.
function FilteredSelect({
  id,
  label,
  value,
  onChange,
  filter,
  loading,
  options,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  filter: string;
  loading?: boolean;
  options: { code: string; name: string }[];
}) {
  const q = filter.trim().toLowerCase();
  const filtered = q
    ? options.filter(
        (o) =>
          o.name.toLowerCase().includes(q) ||
          o.code.toLowerCase().includes(q),
      )
    : options;

  return (
    <div className="flex flex-col gap-1.5">
      <label
        htmlFor={id}
        className="text-sm font-medium text-text-secondary"
      >
        {label}
        {q && (
          <span className="ml-2 text-[10px] font-normal text-text-muted">
            {filtered.length}/{options.length}
          </span>
        )}
      </label>
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required
        className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
      >
        <option value="" disabled>
          {loading ? "Cargando…" : "Elige una opción…"}
        </option>
        {filtered.map((o) => (
          <option key={o.code} value={o.code}>
            {o.name} ({o.code})
          </option>
        ))}
      </select>
    </div>
  );
}

export default function LibrariesAdmin() {
  const { t } = useTranslation();
  const { data: libraries, isLoading, error } = useLibraries();
  const createLibrary = useCreateLibrary();
  const updateLibrary = useUpdateLibrary();
  const scanLibrary = useScanLibrary();
  const refreshM3U = useRefreshM3U();
  const refreshEPG = useRefreshEPG();
  const deleteLibrary = useDeleteLibrary();
  const refreshImages = useRefreshLibraryImages();

  const [showAddModal, setShowAddModal] = useState(false);
  const [refreshMessage, setRefreshMessage] = useState<{ type: "success" | "error"; text: string; libId: string } | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Library | null>(null);
  const [editTarget, setEditTarget] = useState<Library | null>(null);

  // Add library form state
  const [newName, setNewName] = useState("");
  const [newType, setNewType] = useState<ContentType>("movies");
  const [newPath, setNewPath] = useState("");
  const [showCreateBrowse, setShowCreateBrowse] = useState(false);
  // livetv-specific: "public" = iptv-org country picker, "custom" = paste URLs.
  const [newLiveSource, setNewLiveSource] = useState<"public" | "custom">(
    "public",
  );
  // iptv-org supports four URL families: countries/categories/languages/regions.
  // Keeping one state per kind lets the user switch tabs without losing typed
  // input, and the single search filter `newLiveFilter` scopes to whatever is
  // currently active.
  const [newLiveKind, setNewLiveKind] = useState<
    "country" | "category" | "language" | "region"
  >("country");
  const [newLiveFilter, setNewLiveFilter] = useState("");
  const [newCountry, setNewCountry] = useState("");
  const [newLivePick, setNewLivePick] = useState("");
  const [newM3UURL, setNewM3UURL] = useState("");
  const [newEPGURL, setNewEPGURL] = useState("");

  // Only fires the network request while the Add modal is open AND the user
  // has picked livetv — avoids loading the 200-country list for every admin
  // page view. Declared after the state hooks it depends on so TS's
  // temporal-dead-zone check stays happy.
  const publicCountries = usePublicCountries({
    enabled: showAddModal && newType === "livetv" && newLiveSource === "public",
  });

  // Edit library form state
  const [editName, setEditName] = useState("");
  const [editPath, setEditPath] = useState("");
  const [editM3UURL, setEditM3UURL] = useState("");
  const [editEPGURL, setEditEPGURL] = useState("");
  const [showEditBrowse, setShowEditBrowse] = useState(false);

  // Auto-clear refresh message after 4 seconds
  useEffect(() => {
    if (!refreshMessage) return;
    const timer = setTimeout(() => setRefreshMessage(null), 4000);
    return () => clearTimeout(timer);
  }, [refreshMessage]);

  function openEditModal(lib: Library) {
    setEditTarget(lib);
    setEditName(lib.name);
    setEditPath((lib.paths ?? [])[0] ?? "");
    setEditM3UURL(lib.m3u_url ?? "");
    setEditEPGURL(lib.epg_url ?? "");
  }

  function resetAddForm() {
    setNewName("");
    setNewType("movies");
    setNewPath("");
    setNewLiveSource("public");
    setNewLiveKind("country");
    setNewLiveFilter("");
    setNewCountry("");
    setNewLivePick("");
    setNewM3UURL("");
    setNewEPGURL("");
  }

  function handleCreate(e: FormEvent) {
    e.preventDefault();
    if (!newName.trim()) return;

    if (newType === "livetv") {
      // Resolve the M3U URL depending on the chosen source.
      let m3uURL = "";
      if (newLiveSource === "public") {
        const pick = newLiveKind === "country" ? newCountry : newLivePick;
        if (!pick) return;
        // iptv-org URL family map. Countries are under /iptv/countries/ by
        // ISO code; categories/languages/regions each have their own path.
        const pathByKind: Record<typeof newLiveKind, string> = {
          country: "countries",
          category: "categories",
          language: "languages",
          region: "regions",
        };
        m3uURL = `https://iptv-org.github.io/iptv/${pathByKind[newLiveKind]}/${pick}.m3u`;
      } else {
        if (!newM3UURL.trim()) return;
        m3uURL = newM3UURL.trim();
      }
      createLibrary.mutate(
        {
          name: newName.trim(),
          content_type: "livetv",
          paths: [],
          m3u_url: m3uURL,
          epg_url: newEPGURL.trim() || undefined,
        },
        {
          onSuccess: (lib) => {
            setShowAddModal(false);
            resetAddForm();
            // Auto-trigger the first M3U refresh so the library isn't empty
            // the moment the admin closes the modal.
            refreshM3U.mutate(lib.id);
          },
        },
      );
      return;
    }

    // Non-livetv: path-based library.
    if (!newPath.trim()) return;
    createLibrary.mutate(
      { name: newName.trim(), content_type: newType, paths: [newPath.trim()] },
      {
        onSuccess: () => {
          setShowAddModal(false);
          resetAddForm();
        },
      },
    );
  }

  function handleEdit(e: FormEvent) {
    e.preventDefault();
    if (!editTarget || !editName.trim()) return;

    if (editTarget.content_type === "livetv") {
      if (!editM3UURL.trim()) return;
      updateLibrary.mutate(
        {
          id: editTarget.id,
          data: {
            name: editName.trim(),
            m3u_url: editM3UURL.trim(),
            // Explicit empty string clears; if the admin wants to preserve
            // we still send the trimmed value (which is identical to current).
            epg_url: editEPGURL.trim(),
          },
        },
        { onSuccess: () => setEditTarget(null) },
      );
      return;
    }

    // Non-livetv: path edit.
    if (!editPath.trim()) return;
    updateLibrary.mutate(
      {
        id: editTarget.id,
        data: { name: editName.trim(), paths: [editPath.trim()] },
      },
      {
        onSuccess: () => setEditTarget(null),
      },
    );
  }

  function handleDelete() {
    if (!deleteTarget) return;
    deleteLibrary.mutate(deleteTarget.id, {
      onSuccess: () => setDeleteTarget(null),
    });
  }

  if (isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <EmptyState
        title={t('admin.libraries.failedToLoad')}
        description={error.message}
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-text-primary">
          {t('admin.libraries.title')}
        </h2>
        <Button onClick={() => setShowAddModal(true)}>{t('admin.libraries.addLibrary')}</Button>
      </div>

      {/* Refresh images feedback */}
      {refreshMessage && (
        <div
          className={[
            "rounded-[--radius-md] px-4 py-2 text-sm",
            refreshMessage.type === "success"
              ? "bg-success/10 text-success"
              : "bg-error/10 text-error",
          ].join(" ")}
        >
          {refreshMessage.text}
        </div>
      )}

      {/* Table */}
      {libraries && libraries.length > 0 ? (
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">{t('admin.libraries.name')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.libraries.type')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.libraries.path')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.libraries.itemCount')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.libraries.scanStatus')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.libraries.actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {libraries.map((lib) => (
                <tr
                  key={lib.id}
                  className="bg-bg-card hover:bg-bg-elevated transition-colors"
                >
                  <td className="px-4 py-3 font-medium text-text-primary">
                    {lib.name}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={contentTypeBadge(lib.content_type)}>
                      {lib.content_type}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-text-secondary font-mono text-xs">
                    {(lib.paths ?? []).join(", ")}
                  </td>
                  <td className="px-4 py-3 text-right text-text-secondary tabular-nums">
                    {lib.item_count}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={scanStatusVariant(lib.scan_status)}>
                      {lib.scan_status}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      {lib.content_type === "livetv" ? (
                        // ── Live TV row: refresh M3U + refresh EPG ──
                        // Filesystem scan and metadata/image refresh don't
                        // apply here; showing them would just yield dead
                        // buttons, so we route to the IPTV-specific
                        // actions instead.
                        <>
                          <Button
                            variant="secondary"
                            size="sm"
                            isLoading={
                              refreshM3U.isPending && refreshM3U.variables === lib.id
                            }
                            onClick={() =>
                              refreshM3U.mutate(lib.id, {
                                onSuccess: (data) =>
                                  setRefreshMessage({
                                    type: "success",
                                    text: t('admin.libraries.refreshM3USuccess', {
                                      defaultValue: `{{count}} canales importados`,
                                      count: data.channels_imported,
                                    }),
                                    libId: lib.id,
                                  }),
                                onError: () =>
                                  setRefreshMessage({
                                    type: "error",
                                    text: t('admin.libraries.refreshM3UFailed', {
                                      defaultValue: 'No se pudo refrescar el M3U.',
                                    }),
                                    libId: lib.id,
                                  }),
                              })
                            }
                            title={lib.m3u_url || undefined}
                          >
                            {t('admin.libraries.refreshM3U', {
                              defaultValue: 'Actualizar canales',
                            })}
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            isLoading={
                              refreshEPG.isPending && refreshEPG.variables === lib.id
                            }
                            disabled={!lib.epg_url}
                            onClick={() =>
                              refreshEPG.mutate(lib.id, {
                                onSuccess: (data) =>
                                  setRefreshMessage({
                                    type: "success",
                                    text: t('admin.libraries.refreshEPGSuccess', {
                                      defaultValue: `{{count}} programas importados`,
                                      count: data.programs_imported,
                                    }),
                                    libId: lib.id,
                                  }),
                                onError: () =>
                                  setRefreshMessage({
                                    type: "error",
                                    text: t('admin.libraries.refreshEPGFailed', {
                                      defaultValue: 'No se pudo refrescar la guía EPG.',
                                    }),
                                    libId: lib.id,
                                  }),
                              })
                            }
                            title={
                              lib.epg_url ||
                              t('admin.libraries.noEPGURL', {
                                defaultValue:
                                  'No hay URL EPG configurada en esta biblioteca.',
                              })
                            }
                          >
                            {t('admin.libraries.refreshEPG', {
                              defaultValue: 'Actualizar EPG',
                            })}
                          </Button>
                        </>
                      ) : (
                        // ── Regular media library: scan + metadata + images ──
                        <>
                          <Button
                            variant="secondary"
                            size="sm"
                            isLoading={
                              scanLibrary.isPending &&
                              scanLibrary.variables?.id === lib.id &&
                              !scanLibrary.variables?.refreshMetadata
                            }
                            disabled={lib.scan_status === "scanning"}
                            onClick={() => scanLibrary.mutate({ id: lib.id })}
                          >
                            {t('admin.libraries.scan')}
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            isLoading={
                              scanLibrary.isPending &&
                              scanLibrary.variables?.id === lib.id &&
                              !!scanLibrary.variables?.refreshMetadata
                            }
                            disabled={lib.scan_status === "scanning"}
                            onClick={() => scanLibrary.mutate({ id: lib.id, refreshMetadata: true })}
                            title={t('admin.libraries.refreshMetadataTooltip')}
                          >
                            {t('admin.libraries.refreshMetadata')}
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            isLoading={
                              refreshImages.isPending &&
                              refreshImages.variables?.libraryId === lib.id
                            }
                            disabled={lib.scan_status === "scanning"}
                            onClick={() =>
                              refreshImages.mutate(
                                { libraryId: lib.id },
                                {
                                  onSuccess: (data) =>
                                    setRefreshMessage({
                                      type: "success",
                                      text: t('admin.libraries.refreshImagesSuccess', { count: data.updated }),
                                      libId: lib.id,
                                    }),
                                  onError: () =>
                                    setRefreshMessage({
                                      type: "error",
                                      text: t('admin.libraries.refreshImagesFailed'),
                                      libId: lib.id,
                                    }),
                                },
                              )
                            }
                          >
                            {t('admin.libraries.refreshImages')}
                          </Button>
                        </>
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => openEditModal(lib)}
                      >
                        {t('common.edit')}
                      </Button>
                      <Button
                        variant="danger"
                        size="sm"
                        onClick={() => setDeleteTarget(lib)}
                      >
                        {t('common.delete')}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <EmptyState
          title={t('admin.libraries.noLibraries')}
          description={t('admin.libraries.noLibrariesHint')}
          action={
            <Button onClick={() => setShowAddModal(true)}>
              {t('admin.libraries.addLibrary')}
            </Button>
          }
        />
      )}

      {/* Add Library Modal */}
      <Modal
        isOpen={showAddModal}
        onClose={() => setShowAddModal(false)}
        title={t('admin.libraries.addLibrary')}
      >
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <Input
            label={t('admin.libraries.name')}
            placeholder={t('admin.libraries.namePlaceholder')}
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            required
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="content-type"
              className="text-sm font-medium text-text-secondary"
            >
              {t('admin.libraries.contentType')}
            </label>
            <select
              id="content-type"
              value={newType}
              onChange={(e) => setNewType(e.target.value as ContentType)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              {CONTENT_TYPES.map((ct) => (
                <option key={ct.value} value={ct.value}>
                  {t(ct.key)}
                </option>
              ))}
            </select>
          </div>

          {newType === "livetv" ? (
            <>
              {/* Source tabs — Público (iptv-org) vs Personalizada */}
              <div
                role="tablist"
                aria-label={t('admin.libraries.livetvSource', {
                  defaultValue: 'Fuente',
                })}
                className="flex gap-1 rounded-[--radius-md] border border-border bg-bg-surface p-1"
              >
                <button
                  type="button"
                  role="tab"
                  aria-selected={newLiveSource === "public"}
                  onClick={() => setNewLiveSource("public")}
                  className={[
                    "flex-1 rounded-[--radius-sm] px-3 py-1.5 text-xs font-medium transition-colors",
                    newLiveSource === "public"
                      ? "bg-accent/15 text-accent"
                      : "text-text-secondary hover:text-text-primary",
                  ].join(" ")}
                >
                  {t('admin.libraries.livetvPublic', {
                    defaultValue: 'Público (iptv-org)',
                  })}
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={newLiveSource === "custom"}
                  onClick={() => setNewLiveSource("custom")}
                  className={[
                    "flex-1 rounded-[--radius-sm] px-3 py-1.5 text-xs font-medium transition-colors",
                    newLiveSource === "custom"
                      ? "bg-accent/15 text-accent"
                      : "text-text-secondary hover:text-text-primary",
                  ].join(" ")}
                >
                  {t('admin.libraries.livetvCustom', {
                    defaultValue: 'Personalizada',
                  })}
                </button>
              </div>

              {newLiveSource === "public" ? (
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
                        aria-selected={newLiveKind === k}
                        onClick={() => {
                          setNewLiveKind(k);
                          setNewLiveFilter("");
                          setNewCountry("");
                          setNewLivePick("");
                        }}
                        className={[
                          "rounded-[--radius-sm] px-2 py-1 font-medium transition-colors",
                          newLiveKind === k
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
                    label={t('admin.libraries.searchList', {
                      defaultValue: 'Filtrar',
                    })}
                    placeholder={t('admin.libraries.searchListPlaceholder', {
                      defaultValue: 'Escribe para filtrar…',
                    })}
                    value={newLiveFilter}
                    onChange={(e) => setNewLiveFilter(e.target.value)}
                  />

                  {/* The actual picker. Separate branches keep each select's
                      options and selected value independent — switching tabs
                      doesn't wipe the filter but does reset the pick. */}
                  {newLiveKind === "country" ? (
                    <FilteredSelect
                      id="livetv-country"
                      label={t('admin.libraries.country', { defaultValue: 'País' })}
                      value={newCountry}
                      onChange={setNewCountry}
                      filter={newLiveFilter}
                      loading={publicCountries.isLoading}
                      options={(publicCountries.data ?? []).map((c) => ({
                        code: c.code,
                        name: `${c.flag} ${c.name}`,
                      }))}
                    />
                  ) : newLiveKind === "category" ? (
                    <FilteredSelect
                      id="livetv-category"
                      label="Categoría"
                      value={newLivePick}
                      onChange={setNewLivePick}
                      filter={newLiveFilter}
                      options={IPTV_ORG_CATEGORIES}
                    />
                  ) : newLiveKind === "language" ? (
                    <FilteredSelect
                      id="livetv-language"
                      label="Idioma"
                      value={newLivePick}
                      onChange={setNewLivePick}
                      filter={newLiveFilter}
                      options={IPTV_ORG_LANGUAGES}
                    />
                  ) : (
                    <FilteredSelect
                      id="livetv-region"
                      label="Región"
                      value={newLivePick}
                      onChange={setNewLivePick}
                      filter={newLiveFilter}
                      options={IPTV_ORG_REGIONS}
                    />
                  )}

                  <p className="text-[11px] text-text-muted">
                    {t('admin.libraries.publicIPTVHint', {
                      defaultValue:
                        'Playlists públicas del proyecto iptv-org. Puedes añadir varias (p. ej. España + Francia + Deportes).',
                    })}
                  </p>
                </div>
              ) : (
                <>
                  <Input
                    label={t('admin.libraries.m3uUrl', {
                      defaultValue: 'URL M3U',
                    })}
                    placeholder="https://ejemplo.com/playlist.m3u"
                    value={newM3UURL}
                    onChange={(e) => setNewM3UURL(e.target.value)}
                    required
                  />
                </>
              )}

              <Input
                label={t('admin.libraries.epgUrl', {
                  defaultValue: 'URL EPG (opcional)',
                })}
                placeholder="https://ejemplo.com/epg.xml"
                value={newEPGURL}
                onChange={(e) => setNewEPGURL(e.target.value)}
              />
              <p className="-mt-2 text-[11px] text-text-muted">
                {t('admin.libraries.epgURLHint', {
                  defaultValue:
                    'Si el M3U trae url-tvg en su cabecera, se auto-detecta.',
                })}
              </p>
            </>
          ) : (
            <div className="flex gap-2 items-end">
              <div className="flex-1">
                <Input
                  label={t('admin.libraries.path')}
                  placeholder={t('admin.libraries.pathPlaceholder')}
                  value={newPath}
                  onChange={(e) => setNewPath(e.target.value)}
                  required
                />
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setShowCreateBrowse(true)}
              >
                {t('common.browse')}
              </Button>
            </div>
          )}

          {createLibrary.error && (
            <p className="text-xs text-error">{createLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setShowAddModal(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" isLoading={createLibrary.isPending}>
              {t('common.create')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Edit Library Modal */}
      <Modal
        isOpen={editTarget !== null}
        onClose={() => setEditTarget(null)}
        title={t('admin.libraries.editLibrary')}
      >
        <form onSubmit={handleEdit} className="flex flex-col gap-4">
          <Input
            label={t('admin.libraries.name')}
            placeholder={t('admin.libraries.namePlaceholder')}
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            required
          />

          {editTarget?.content_type === "livetv" ? (
            <>
              <Input
                label={t('admin.libraries.m3uUrl', {
                  defaultValue: 'URL M3U',
                })}
                placeholder="https://ejemplo.com/playlist.m3u"
                value={editM3UURL}
                onChange={(e) => setEditM3UURL(e.target.value)}
                required
              />
              <Input
                label={t('admin.libraries.epgUrl', {
                  defaultValue: 'URL EPG (opcional)',
                })}
                placeholder="https://ejemplo.com/epg.xml"
                value={editEPGURL}
                onChange={(e) => setEditEPGURL(e.target.value)}
              />
              <p className="-mt-2 text-[11px] text-text-muted">
                {t('admin.libraries.epgURLHint', {
                  defaultValue:
                    'Si el M3U trae url-tvg en su cabecera, se auto-detecta.',
                })}
              </p>
            </>
          ) : (
            <div className="flex gap-2 items-end">
              <div className="flex-1">
                <Input
                  label={t('admin.libraries.path')}
                  placeholder={t('admin.libraries.pathPlaceholder')}
                  value={editPath}
                  onChange={(e) => setEditPath(e.target.value)}
                  required
                />
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setShowEditBrowse(true)}
              >
                {t('common.browse')}
              </Button>
            </div>
          )}

          {updateLibrary.error && (
            <p className="text-xs text-error">{updateLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setEditTarget(null)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" isLoading={updateLibrary.isPending}>
              {t('common.save')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('admin.libraries.deleteLibrary')}
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            {t('admin.libraries.deleteConfirm', { name: deleteTarget?.name })}
          </p>

          {deleteLibrary.error && (
            <p className="text-xs text-error">
              {deleteLibrary.error.message}
            </p>
          )}

          <div className="flex justify-end gap-3">
            <Button
              variant="secondary"
              onClick={() => setDeleteTarget(null)}
            >
              {t('common.cancel')}
            </Button>
            <Button
              variant="danger"
              isLoading={deleteLibrary.isPending}
              onClick={handleDelete}
            >
              {t('common.delete')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Folder Browsers */}
      <FolderBrowser
        isOpen={showCreateBrowse}
        onClose={() => setShowCreateBrowse(false)}
        onSelect={(path) => {
          setNewPath(path);
          if (!newName.trim()) {
            const segments = path.split("/").filter(Boolean);
            setNewName(segments[segments.length - 1] ?? "");
          }
        }}
        useAdmin
      />

      <FolderBrowser
        isOpen={showEditBrowse}
        onClose={() => setShowEditBrowse(false)}
        onSelect={(path) => setEditPath(path)}
        useAdmin
      />
    </div>
  );
}
