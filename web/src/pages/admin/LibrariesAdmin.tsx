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
  const [newCountry, setNewCountry] = useState("");
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
    setNewCountry("");
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
        if (!newCountry) return;
        m3uURL = `https://iptv-org.github.io/iptv/countries/${newCountry}.m3u`;
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
                <div className="flex flex-col gap-1.5">
                  <label
                    htmlFor="livetv-country"
                    className="text-sm font-medium text-text-secondary"
                  >
                    {t('admin.libraries.country', { defaultValue: 'País' })}
                  </label>
                  <select
                    id="livetv-country"
                    value={newCountry}
                    onChange={(e) => setNewCountry(e.target.value)}
                    required
                    className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
                  >
                    <option value="" disabled>
                      {publicCountries.isLoading
                        ? t('common.loading', { defaultValue: 'Cargando…' })
                        : t('admin.libraries.pickCountry', {
                            defaultValue: 'Elige un país…',
                          })}
                    </option>
                    {(publicCountries.data ?? []).map((c) => (
                      <option key={c.code} value={c.code}>
                        {c.flag} {c.name}
                      </option>
                    ))}
                  </select>
                  <p className="text-[11px] text-text-muted">
                    {t('admin.libraries.publicIPTVHint', {
                      defaultValue:
                        'Se usará la playlist del proyecto iptv-org para ese país.',
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
